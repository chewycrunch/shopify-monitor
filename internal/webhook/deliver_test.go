package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// recorder captures the requests a webhook received and when each arrived.
type recorder struct {
	mu     sync.Mutex
	times  []time.Time
	bodies []string
}

func (r *recorder) note(body string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.times = append(r.times, time.Now())
	r.bodies = append(r.bodies, body)
	return len(r.times)
}

// variantIDRe pulls the variant identifier out of a rendered alert. Matching a
// bare id against the whole payload would also match the year in its timestamp.
var variantIDRe = regexp.MustCompile(`"Variant ID","value":"(\d+)"`)

// delivered lists the variant ids this webhook actually received, in order.
func (r *recorder) delivered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for _, b := range r.bodies {
		if m := variantIDRe.FindStringSubmatch(b); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.times)
}

// gapAfterFirst is how long the sender waited between its first and second
// requests, which is where every pacing rule shows itself.
func (r *recorder) gapAfterFirst() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.times) < 2 {
		return 0
	}
	return r.times[1].Sub(r.times[0])
}

// webhookServer stands in for one Discord webhook. handler receives the
// 1-based attempt number and writes the response Discord would have sent.
func webhookServer(t *testing.T, handler func(n int, w http.ResponseWriter)) (*recorder, string) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		handler(rec.note(string(body)), w)
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL + "/api/webhooks/1542630725822578870/testtoken"
}

// ok answers as an accepted delivery reporting plenty of remaining allowance.
func ok(w http.ResponseWriter) {
	w.Header().Set("X-RateLimit-Remaining", "4")
	w.Header().Set("X-RateLimit-Reset-After", "1.0")
	w.WriteHeader(http.StatusNoContent)
}

// bare answers as an accepted delivery reporting no allowance at all, which the
// documentation permits.
func bare(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testDiscord(t *testing.T, url string, opts Options) *discord {
	t.Helper()
	if opts.BaseBackoff == 0 {
		opts.BaseBackoff = time.Millisecond
	}
	d, err := newDiscord(url, opts)
	if err != nil {
		t.Fatalf("newDiscord: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.shutdown(ctx); err != nil {
			t.Logf("shutting down the test destination: %v", err)
		}
	})
	return d
}

// A cycle can produce hundreds of events and the webhook accepts a few per
// second. Waiting for delivery here would stall the crawl behind it.
//
// @spec NOTIFY-QUEUE-001, NOTIFY-QUEUE-005
func TestSendReturnsWithoutWaitingForDelivery(t *testing.T) {
	release := make(chan struct{})
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		<-release
		ok(w)
	})
	defer close(release)

	d := testDiscord(t, url, Options{})

	start := time.Now()
	if err := d.Send(context.Background(), testEvent(utils.Restock)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Send took %s while the webhook was hanging; it waited for delivery", elapsed)
	}
	waitFor(t, "the event to be attempted", func() bool { return rec.count() >= 1 })
}

// @spec NOTIFY-QUEUE-006
func TestEventsAreDeliveredInTheOrderTheyWereAccepted(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) { ok(w) })
	d := testDiscord(t, url, Options{})

	for _, id := range []int64{101, 102, 103} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	waitFor(t, "all three deliveries", func() bool { return rec.count() >= 3 })

	if got := strings.Join(rec.delivered(), ","); got != "101,102,103" {
		t.Errorf("delivered in order %s, want 101,102,103", got)
	}
}

// A restock alert delivered ten minutes late has been acted on by everyone it
// would have helped, so the newest event is the one worth the slot.
//
// @spec NOTIFY-QUEUE-002, NOTIFY-QUEUE-003, NOTIFY-QUEUE-004
func TestAFullQueueDropsItsOldestEvent(t *testing.T) {
	release := make(chan struct{})
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		<-release
		ok(w)
	})

	d := testDiscord(t, url, Options{QueueDepth: 2})

	send := func(id int64) {
		t.Helper()
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		start := time.Now()
		if err := d.Send(context.Background(), e); err != nil {
			t.Logf("Send(%d) refused: %v", id, err)
		}
		if time.Since(start) > 100*time.Millisecond {
			t.Fatalf("Send(%d) blocked on a full queue; it must drop rather than push back", id)
		}
	}

	// Park the first event in the sender before filling the queue behind it.
	// Without this the sender may or may not have dequeued it yet, and which
	// events remain queued would depend on that timing.
	send(201)
	waitFor(t, "the first event to reach the webhook", func() bool { return rec.count() >= 1 })

	// The queue holds two. Each of these evicts the longest-waiting one, so
	// 202, 203 and 204 are pushed out and 205, 206 remain.
	for _, id := range []int64{202, 203, 204, 205, 206} {
		send(id)
	}

	close(release)
	waitFor(t, "the queue to drain", func() bool { return rec.count() >= 3 })
	time.Sleep(50 * time.Millisecond)

	got := rec.delivered()
	want := []string{"201", "205", "206"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("delivered %v, want %v: the event in flight, plus the two newest the queue kept", got, want)
	}
}

// @spec NOTIFY-DELIVER-006
func TestATransientFailureIsRetried(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{})
	if err := d.Send(context.Background(), testEvent(utils.Restock)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	waitFor(t, "the delivery to succeed after retries", func() bool { return rec.count() >= 3 })
	time.Sleep(30 * time.Millisecond)
	if got := rec.count(); got > deliveryAttempts {
		t.Errorf("made %d attempts, want no more than %d", got, deliveryAttempts)
	}
}

// A malformed request cannot be fixed by sending it again, and the events
// behind it are not implicated.
//
// @spec NOTIFY-DELIVER-007
func TestAnUnretryableRefusalDropsOnlyThatEvent(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{})

	first := testEvent(utils.Restock)
	first.Variant.ID = 301
	second := testEvent(utils.Restock)
	second.Variant.ID = 302
	for _, e := range []utils.Event{first, second} {
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	waitFor(t, "the second event to be delivered", func() bool { return rec.count() >= 2 })
	time.Sleep(30 * time.Millisecond)

	if rec.count() != 2 {
		t.Errorf("made %d requests; a 400 was retried instead of dropped", rec.count())
	}
	if got := rec.delivered(); len(got) == 0 || got[len(got)-1] != "302" {
		t.Errorf("delivered %v; the event behind the refused one did not go out", got)
	}
}

// Being rate limited is the normal working state of a busy webhook, not a
// symptom of a broken one.
//
// @spec NOTIFY-DELIVER-008
func TestRateLimitingNeverDisablesADestination(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		// Comfortably more refusals than the failure threshold allows.
		if n <= DefaultMaxFailures+5 {
			w.Header().Set("Retry-After", "0.001")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{})
	for i := range 40 {
		e := testEvent(utils.Restock)
		e.Variant.ID = int64(400 + i)
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d) was refused while merely rate limited: %v", e.Variant.ID, err)
		}
		time.Sleep(time.Millisecond)
	}

	waitFor(t, "a delivery to get through the rate limiting", func() bool {
		return rec.count() > DefaultMaxFailures+5
	})
}

// @spec NOTIFY-DELIVER-001, NOTIFY-DISCORD-008
func TestAStatedWaitIntervalIsObeyedOverAnyReportedAllowance(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			// A long allowance reset alongside a short stated wait: the stated
			// wait is later information and must win.
			w.Header().Set("X-RateLimit-Reset-After", "30.0")
			w.Header().Set("Retry-After", "0.15")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{})
	if err := d.Send(context.Background(), testEvent(utils.Restock)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	waitFor(t, "the retry after the stated interval", func() bool { return rec.count() >= 2 })

	if gap := rec.gapAfterFirst(); gap < 140*time.Millisecond {
		t.Errorf("retried after %s, before the 150ms Discord asked for", gap)
	}
	if gap := rec.gapAfterFirst(); gap > 5*time.Second {
		t.Errorf("waited %s, following the allowance reset instead of the stated interval", gap)
	}
}

// The endpoint's own accounting is exact, so pacing by it neither idles below
// the limit nor discovers it by exceeding it.
//
// @spec NOTIFY-DELIVER-009, NOTIFY-DISCORD-006
func TestAnExhaustedAllowanceIsWaitedOut(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset-After", "0.2")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{})
	for _, id := range []int64{501, 502} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	waitFor(t, "the second delivery", func() bool { return rec.count() >= 2 })

	if gap := rec.gapAfterFirst(); gap < 190*time.Millisecond {
		t.Errorf("second request went out %s after the first, though the allowance was spent for 200ms", gap)
	}
}

// The allowance headers are documented as optional, so a sender that only works
// when they are present would fail intermittently and silently.
//
// @spec NOTIFY-DELIVER-002, NOTIFY-DELIVER-010
func TestWithoutAReportedAllowanceTheConfiguredIntervalPaces(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) { bare(w) })

	d := testDiscord(t, url, Options{MinInterval: 150 * time.Millisecond})
	for _, id := range []int64{601, 602} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	waitFor(t, "the second delivery", func() bool { return rec.count() >= 2 })

	if gap := rec.gapAfterFirst(); gap < 140*time.Millisecond {
		t.Errorf("requests were %s apart with a 150ms minimum interval configured", gap)
	}
}

// A destination that reports its allowance needs no local guess, so the default
// interval buys nothing and costs latency.
//
// @spec NOTIFY-DELIVER-004, NOTIFY-DELIVER-005
func TestByDefaultNothingIsAddedBetweenRequests(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) { ok(w) })

	d := testDiscord(t, url, Options{})
	for _, id := range []int64{701, 702} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	waitFor(t, "the second delivery", func() bool { return rec.count() >= 2 })

	if gap := rec.gapAfterFirst(); gap > 100*time.Millisecond {
		t.Errorf("requests were %s apart with no interval configured and allowance to spare", gap)
	}
}
