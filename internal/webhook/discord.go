package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// The lengths Discord accepts. An overrun is refused, and a refused alert is
// worth less than one naming its product by an abbreviated title.
const (
	discordTitleLimit      = 256
	discordFieldValueLimit = 1024
)

// Embed colours, distinguishing the two reportable kinds before the text is
// read. They live here rather than in the segment's own vocabulary because a
// colour integer means nothing to a destination that has no colours.
const (
	colourRestock    = 0x2ECC71
	colourNewVariant = 0x3498DB
)

// discordHosts are the hosts serving Discord webhooks.
var discordHosts = []string{"discord.com", "discordapp.com"}

// webhookPath is the path segment every Discord webhook URL carries.
const webhookPath = "/api/webhooks/"

// rateLimitFallback paces retries when a webhook refuses for rate without
// saying for how long. Without it a refusal carrying no interval would be
// retried in a tight loop.
const rateLimitFallback = time.Second

// dropReportInterval throttles overflow reporting. A queue that is overflowing
// is overflowing on nearly every event, and a line per dropped event would bury
// the alerts that did get through.
const dropReportInterval = 5 * time.Second

// discord delivers events to one Discord webhook.
//
// One instance exists per webhook URL, holding the single queue, sender, and
// rate-limit state that every store naming that URL shares.
type discord struct {
	rawURL string
	id     string
	opts   Options
	log    *slog.Logger

	queue chan utils.Event

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu          sync.Mutex
	closed      bool
	disabled    bool
	dropped     int
	lastDropLog time.Time

	// Written only by the sender goroutine.
	nextAllowed time.Time
	discarded   int
	failures    int
}

// newDiscord builds a destination for one Discord webhook URL and starts its
// sender.
func newDiscord(rawURL string, opts Options) (*discord, error) {
	opts = opts.withDefaults()

	id, err := webhookID(rawURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &discord{
		rawURL: rawURL,
		id:     id,
		opts:   opts,
		queue:  make(chan utils.Event, opts.QueueDepth),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	d.log = opts.Logger.With("destination", d.Name())

	go d.run()

	return d, nil
}

// handlesDiscord reports whether rawURL addresses a Discord webhook.
//
// @spec NOTIFY-DISCORD-001
func handlesDiscord(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.Contains(u.Path, webhookPath) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range discordHosts {
		if host == h {
			return true
		}
	}
	return false
}

// webhookID pulls the webhook's identifier out of its URL, leaving the token
// behind.
func webhookID(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("webhook: %q is not a valid URL: %w", rawURL, err)
	}
	i := strings.Index(u.Path, webhookPath)
	if i < 0 {
		return "", ErrUnrecognised
	}
	rest := strings.Trim(u.Path[i+len(webhookPath):], "/")
	id, _, _ := strings.Cut(rest, "/")
	if id == "" {
		return "", ErrUnrecognised
	}
	return id, nil
}

// Name identifies this destination in logs by kind and webhook id, never by the
// token the URL carries — a log line quoting one hands out the right to post.
//
// @spec NOTIFY-DEST-004, NOTIFY-DISCORD-005
func (d *discord) Name() string { return "discord:" + d.id }

// Send accepts an event for delivery, returning without waiting for it.
//
// A full queue gives up its oldest entry rather than pushing back: blocking
// would pace the crawl by the webhook, and a restock alert delivered ten
// minutes late has been acted on by everyone it would have helped.
//
// @spec NOTIFY-QUEUE-001, NOTIFY-QUEUE-002, NOTIFY-QUEUE-003, NOTIFY-QUEUE-004, NOTIFY-QUEUE-006, NOTIFY-FAIL-005
func (d *discord) Send(_ context.Context, e utils.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disabled {
		return fmt.Errorf("%s: destination is disabled", d.Name())
	}
	if d.closed {
		return fmt.Errorf("%s: destination is shut down", d.Name())
	}

	select {
	case d.queue <- e:
		return nil
	default:
	}

	// Full. Discard the longest-waiting event to make room for the newest.
	select {
	case <-d.queue:
		d.noteDroppedLocked()
	default:
	}

	select {
	case d.queue <- e:
		return nil
	default:
		d.noteDroppedLocked()
		return fmt.Errorf("%s: queue full", d.Name())
	}
}

// noteDroppedLocked counts an overflow discard and reports the running total,
// no more often than dropReportInterval.
func (d *discord) noteDroppedLocked() {
	d.dropped++
	if !d.lastDropLog.IsZero() && time.Since(d.lastDropLog) < dropReportInterval {
		return
	}
	d.lastDropLog = time.Now()
	d.log.Warn("discarding undelivered events, queue is full", "discarded_total", d.dropped)
}

// run delivers queued events until the queue closes.
//
// Once the context is cancelled — a disabled destination, or a shutdown that
// ran out of time — remaining events are drained without being sent, so the
// goroutine ends promptly rather than posting into a webhook that cannot
// receive them.
func (d *discord) run() {
	defer close(d.done)

	for e := range d.queue {
		if d.ctx.Err() != nil {
			d.discarded++
			continue
		}
		d.attempt(e)
	}
}

// attempt delivers one event, retrying as the response warrants.
//
// @spec NOTIFY-DELIVER-001, NOTIFY-DELIVER-002, NOTIFY-DELIVER-006, NOTIFY-DELIVER-007, NOTIFY-DELIVER-008, NOTIFY-DELIVER-009, NOTIFY-DELIVER-010, NOTIFY-DISCORD-006, NOTIFY-DISCORD-007, NOTIFY-DISCORD-008, NOTIFY-FAIL-001, NOTIFY-FAIL-002, NOTIFY-FAIL-003, NOTIFY-FAIL-004
func (d *discord) attempt(e utils.Event) {
	body, err := json.Marshal(NewPayload().SetEmbeds(&[]Embed{buildEmbed(e)}))
	if err != nil {
		d.log.Error("could not render alert", "err", err)
		return
	}

	backoff := d.opts.BaseBackoff

	for tries := 0; tries < deliveryAttempts; {
		if !d.waitTurn() {
			return
		}

		outcome := d.post(body)
		d.observe(outcome)

		switch outcome.kind {
		case delivered:
			d.succeed()
			return

		case rateLimited:
			// Not a failure: being rate limited is the working state of a busy
			// webhook. It costs no attempt, because the interval Discord named
			// is what makes the next try worth making.
			continue

		case revoked:
			// The webhook is gone or the token no longer works. Asking again
			// returns the same answer, so this counts immediately.
			d.fail(outcome.reason)
			return

		case rejected:
			// The request itself is unacceptable. Retrying cannot change it,
			// and the events behind it are not implicated.
			d.log.Warn("alert refused, dropping it", "reason", outcome.reason)
			return

		case unavailable:
			tries++
			if tries >= deliveryAttempts {
				d.fail(outcome.reason)
				return
			}
			if !d.sleep(backoff) {
				return
			}
			backoff *= 2
		}
	}
}

// waitTurn holds until this destination is due another request.
func (d *discord) waitTurn() bool {
	if wait := time.Until(d.nextAllowed); wait > 0 {
		return d.sleep(wait)
	}
	return true
}

func (d *discord) sleep(dur time.Duration) bool {
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-d.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// observe records when this destination may next be asked for something.
//
// A stated wait interval wins outright: it is later information than the
// allowance reported alongside it, and disregarding it is what escalates a
// per-webhook limit into a block on the host.
func (d *discord) observe(o outcome) {
	now := time.Now()

	switch {
	case o.retryAfter > 0:
		d.nextAllowed = now.Add(o.retryAfter)
	case o.allow.reported && o.allow.remaining <= 0:
		d.nextAllowed = now.Add(o.allow.resetIn)
	default:
		d.nextAllowed = now.Add(d.opts.MinInterval)
	}
}

func (d *discord) succeed() {
	d.failures = 0
}

// fail counts a delivery this destination could not complete, and withdraws
// from it once too many have run together.
func (d *discord) fail(reason string) {
	d.failures++
	d.log.Warn("delivery failed", "reason", reason, "consecutive_failures", d.failures)

	if d.failures < d.opts.MaxFailures {
		return
	}

	d.mu.Lock()
	already := d.disabled
	d.disabled = true
	shouldClose := !d.closed
	d.closed = true
	d.mu.Unlock()

	if already {
		return
	}

	d.log.Error("destination disabled until restart", "reason", reason, "consecutive_failures", d.failures)

	// Abandon what is queued rather than spending requests draining it into a
	// webhook that has stopped accepting them.
	d.cancel()
	if shouldClose {
		close(d.queue)
	}
}

// shutdown stops accepting events and delivers what is already accepted until
// ctx expires, discarding and reporting whatever remains.
//
// @spec NOTIFY-LIFE-001, NOTIFY-LIFE-002, NOTIFY-LIFE-003
func (d *discord) shutdown(ctx context.Context) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		<-d.done
		return nil
	}
	d.closed = true
	close(d.queue)
	d.mu.Unlock()

	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
	}

	d.cancel()
	<-d.done

	if d.discarded > 0 {
		d.log.Warn("discarded undelivered events at shutdown", "discarded", d.discarded)
	}
	return ctx.Err()
}

// outcomeKind is how a webhook answered a delivery.
type outcomeKind int

const (
	delivered outcomeKind = iota
	rateLimited
	unavailable // transient: worth trying again
	revoked     // the webhook is gone or unauthorised
	rejected    // this request is unacceptable, others are not
)

type outcome struct {
	kind       outcomeKind
	reason     string
	retryAfter time.Duration
	allow      allowance
}

// post sends one rendered alert and classifies the answer.
func (d *discord) post(body []byte) outcome {
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, d.rawURL, bytes.NewReader(body))
	if err != nil {
		return outcome{kind: rejected, reason: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.opts.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return outcome{kind: rejected, reason: "shutting down"}
		}
		return outcome{kind: unavailable, reason: err.Error()}
	}
	defer resp.Body.Close()

	o := outcome{allow: parseAllowance(resp.Header), reason: resp.Status}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		o.kind = rateLimited
		o.retryAfter = parseSeconds(resp.Header.Get("Retry-After"))
		if o.retryAfter <= 0 {
			o.retryAfter = rateLimitFallback
		}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		o.kind = delivered
	case resp.StatusCode == http.StatusNotFound,
		resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		o.kind = revoked
	case resp.StatusCode >= 500:
		o.kind = unavailable
	default:
		o.kind = rejected
	}

	return o
}

// allowance is what a response reports about the requests still available on
// this webhook, and when that allowance renews.
type allowance struct {
	remaining int
	resetIn   time.Duration
	reported  bool
}

// parseAllowance reads the budget a response reports. The headers are
// documented as optional, so their absence is expected rather than an error.
func parseAllowance(h http.Header) allowance {
	remaining := h.Get("X-RateLimit-Remaining")
	reset := h.Get("X-RateLimit-Reset-After")
	if remaining == "" || reset == "" {
		return allowance{}
	}

	n, err := strconv.Atoi(remaining)
	if err != nil {
		return allowance{}
	}

	return allowance{remaining: n, resetIn: parseSeconds(reset), reported: true}
}

// parseSeconds reads a possibly fractional count of seconds.
func parseSeconds(v string) time.Duration {
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

// buildEmbed renders one event as a Discord embed.
//
// @spec NOTIFY-ALERT-001, NOTIFY-ALERT-002, NOTIFY-ALERT-003, NOTIFY-ALERT-004, NOTIFY-ALERT-005, NOTIFY-ALERT-006, NOTIFY-ALERT-007, NOTIFY-DISCORD-002, NOTIFY-DISCORD-003, NOTIFY-DISCORD-004
func buildEmbed(e utils.Event) Embed {
	colour := colourNewVariant
	if e.Kind == utils.Restock {
		colour = colourRestock
	}

	var embed Embed
	embed.SetTitle(shorten(e.Product.Title, discordTitleLimit)).
		SetURL(productURL(e)).
		SetColor(colour)

	embed.Timestamp = ptr(e.DetectedAt.UTC().Format(time.RFC3339))

	embed.AddField(EmbedField{Name: "Variant", Value: shorten(e.Variant.Title, discordFieldValueLimit), Inline: true}).
		AddField(EmbedField{Name: "Variant ID", Value: strconv.FormatInt(e.Variant.ID, 10), Inline: true}).
		AddField(EmbedField{Name: "Store", Value: shorten(e.Store, discordFieldValueLimit), Inline: true})

	// A product without an image is still worth alerting on; the image is not
	// what makes it worth sending.
	if len(e.Product.Images) > 0 {
		embed.SetThumbnail(e.Product.Images[0].Src)
	}

	return embed
}

func productURL(e utils.Event) string {
	return strings.TrimRight(e.Store, "/") + "/products/" + e.Product.Handle
}

// shorten trims to a rune count Discord will accept. Discord refuses an
// overlong value outright, and this segment treats that refusal as permanent,
// so an untruncated alert is a lost one.
func shorten(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	if limit <= 1 {
		return string(r[:limit])
	}
	return string(r[:limit-1]) + "…"
}

func ptr[T any](v T) *T { return &v }
