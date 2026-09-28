package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// logCapture collects what a destination reported, so a test can assert that
// something was said once rather than every cycle.
type logCapture struct {
	mu   sync.Mutex
	logs []string
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", r.Level, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, b.String())
	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCapture) WithGroup(string) slog.Handler      { return c }

func (c *logCapture) matching(level slog.Level, substr string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, l := range c.logs {
		if strings.HasPrefix(l, level.String()) && strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return out
}

func captureLogs() (*logCapture, *slog.Logger) {
	c := &logCapture{}
	return c, slog.New(c)
}

// A destination retrying a dead webhook forever keeps its queue permanently
// full, so one bad row in the stores file silences the good rows behind it.
//
// @spec NOTIFY-FAIL-001, NOTIFY-FAIL-002, NOTIFY-FAIL-003, NOTIFY-FAIL-005
func TestADestinationThatKeepsFailingIsDisabledAndSaysSoOnce(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	capture, logger := captureLogs()

	d := testDiscord(t, url, Options{MaxFailures: 3, Logger: logger})

	var lastErr error
	waitFor(t, "the destination to start refusing events", func() bool {
		e := testEvent(utils.Restock)
		lastErr = d.Send(context.Background(), e)
		return lastErr != nil
	})

	if rec.count() == 0 {
		t.Fatal("the destination refused events without ever attempting a delivery")
	}

	reports := capture.matching(slog.LevelError, "disab")
	if len(reports) == 0 {
		t.Errorf("disabling a destination was never reported at error level; logs:\n%s", strings.Join(capture.logs, "\n"))
	}
	if len(reports) > 1 {
		t.Errorf("disabling was reported %d times, want once:\n%s", len(reports), strings.Join(reports, "\n"))
	}
}

// A success is evidence the destination is healthy, so a run of failures either
// side of it is two runs, not one.
//
// @spec NOTIFY-FAIL-001
func TestASuccessfulDeliveryClearsTheFailureRun(t *testing.T) {
	rec, url := webhookServer(t, func(n int, w http.ResponseWriter) {
		// Fail, succeed, fail: never two consecutive failures.
		if n%2 == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ok(w)
	})

	d := testDiscord(t, url, Options{MaxFailures: 2})

	for i := range 12 {
		e := testEvent(utils.Restock)
		e.Variant.ID = int64(800 + i)
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("destination was disabled though no two deliveries failed in a row: %v", err)
		}
		time.Sleep(3 * time.Millisecond)
	}

	waitFor(t, "deliveries to be attempted", func() bool { return rec.count() >= 6 })
}

// Draining a queue into a webhook that cannot receive it wastes the requests
// and delays nothing useful.
//
// @spec NOTIFY-FAIL-004
func TestDisablingADestinationDiscardsWhatItWasHolding(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	d := testDiscord(t, url, Options{MaxFailures: 2, QueueDepth: 64})

	for i := range 40 {
		e := testEvent(utils.Restock)
		e.Variant.ID = int64(900 + i)
		if err := d.Send(context.Background(), e); err != nil {
			t.Logf("Send(%d) refused, the destination is already disabled: %v", e.Variant.ID, err)
		}
	}

	waitFor(t, "the destination to be disabled", func() bool {
		return d.Send(context.Background(), testEvent(utils.Restock)) != nil
	})

	settled := rec.count()
	time.Sleep(100 * time.Millisecond)

	if grew := rec.count() - settled; grew > 0 {
		t.Errorf("%d further requests went out after the destination was disabled; its queue was drained rather than discarded", grew)
	}
}

// A webhook that has been deleted answers the same way however many times it is
// asked, so there is nothing to retry.
//
// @spec NOTIFY-DISCORD-007
func TestADeletedWebhookIsNotRetriedAndCountsAgainstTheDestination(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	})

	d := testDiscord(t, url, Options{MaxFailures: 3})

	waitFor(t, "the destination to be disabled", func() bool {
		return d.Send(context.Background(), testEvent(utils.Restock)) != nil
	})
	time.Sleep(50 * time.Millisecond)

	// Three failed deliveries, one request each — not one request per retry.
	if got := rec.count(); got > 3 {
		t.Errorf("made %d requests to a deleted webhook, want at most one per failed delivery (3)", got)
	}
}

// @spec NOTIFY-FAIL-006
func TestOneDisabledDestinationDoesNotSilenceTheOthers(t *testing.T) {
	_, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	})

	dead := testDiscord(t, url, Options{MaxFailures: 1})
	healthy := &recordingDest{name: "healthy"}

	f, err := Fanout(dead, healthy)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}

	waitFor(t, "the dead destination to be disabled", func() bool {
		return dead.Send(context.Background(), testEvent(utils.Restock)) != nil
	})

	before := healthy.count()
	if err := f.Send(context.Background(), testEvent(utils.Restock)); err == nil {
		t.Error("the disabled destination reported no refusal")
	}

	if healthy.count() != before+1 {
		t.Error("a disabled destination stopped its store's other destinations from receiving the event")
	}
}
