package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// ErrUnrecognised reports a webhook URL that no destination kind handles.
var ErrUnrecognised = errors.New("webhook: no destination handles this URL")

// ErrNoDestinations reports an attempt to compose an empty set of destinations.
// A store with nowhere to report is refused when the stores file is read, so
// reaching Fanout without one is a defect rather than a configuration.
var ErrNoDestinations = errors.New("webhook: no destinations")

// A Destination delivers an event to somewhere an operator watches.
//
// Send enqueues; it does not deliver. Its error reports that the event was not
// accepted — the queue is full, or the destination is disabled or shut down. A
// delivery accepted and later failed is logged by the destination, because by
// then the caller has moved on to the next crawl.
type Destination interface {
	Send(ctx context.Context, e utils.Event) error
	Name() string
}

// Defaults for Options. MinInterval is zero because a destination that reports
// its own request allowance needs no local guess; the interval exists for the
// destinations that report none.
const (
	DefaultQueueDepth  = 256
	DefaultMinInterval = 0
	DefaultMaxFailures = 10
	DefaultBaseBackoff = time.Second

	// deliveryAttempts is the initial try plus the retries allowed after it.
	deliveryAttempts = 4
)

// Options govern every destination a Registry hands out. They are global rather
// than per store because a destination is shared by the stores that name it,
// so per-store values for one webhook could conflict with no correct answer.
type Options struct {
	MinInterval time.Duration
	QueueDepth  int
	MaxFailures int
	BaseBackoff time.Duration
	Client      *http.Client
	Logger      *slog.Logger
}

func (o Options) withDefaults() Options {
	if o.QueueDepth <= 0 {
		o.QueueDepth = DefaultQueueDepth
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = DefaultMaxFailures
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = DefaultBaseBackoff
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Registry hands out destinations, one per unique webhook URL.
//
// Sharing matters because every limit constraining delivery is imposed per
// webhook: independent instances for two stores naming one URL would each pace
// correctly and collectively exceed it.
type Registry struct {
	opts Options

	mu    sync.Mutex
	dests map[string]Destination
}

func NewRegistry(opts Options) *Registry {
	return &Registry{opts: opts.withDefaults(), dests: make(map[string]Destination)}
}

// For returns the destination handling rawURL, creating it on first use.
//
// The same URL always yields the same destination. That sharing is what makes
// the per-webhook budget correct: separate instances for two stores naming one
// URL would each pace correctly and collectively exceed it.
//
// @spec NOTIFY-DEST-001, NOTIFY-DEST-002, NOTIFY-DEST-003
func (r *Registry) For(rawURL string) (Destination, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if d, ok := r.dests[rawURL]; ok {
		return d, nil
	}

	if !handlesDiscord(rawURL) {
		return nil, fmt.Errorf("%q: %w", rawURL, ErrUnrecognised)
	}

	d, err := newDiscord(rawURL, r.opts)
	if err != nil {
		return nil, err
	}

	r.dests[rawURL] = d
	return d, nil
}

// shutdowner is a destination with senders of its own to wind down.
type shutdowner interface {
	shutdown(ctx context.Context) error
}

// Shutdown stops every destination accepting events and delivers what they
// already hold until ctx expires.
//
// @spec NOTIFY-LIFE-001, NOTIFY-LIFE-002, NOTIFY-LIFE-003
func (r *Registry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	dests := make([]Destination, 0, len(r.dests))
	for _, d := range r.dests {
		dests = append(dests, d)
	}
	r.mu.Unlock()

	var errs []error
	for _, d := range dests {
		s, ok := d.(shutdowner)
		if !ok {
			continue
		}
		if err := s.shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Fanout composes destinations into one.
//
// Duplicates are discarded: identical URLs resolve through the registry to the
// same instance, so an operator naming a webhook twice gets one alert. A single
// destination is returned as itself rather than wrapped.
//
// @spec NOTIFY-DEST-005, NOTIFY-DEST-006
func Fanout(ds ...Destination) (Destination, error) {
	seen := make(map[Destination]bool, len(ds))
	unique := make(fanout, 0, len(ds))
	for _, d := range ds {
		if d == nil || seen[d] {
			continue
		}
		seen[d] = true
		unique = append(unique, d)
	}

	switch len(unique) {
	case 0:
		return nil, ErrNoDestinations
	case 1:
		return unique[0], nil
	default:
		return unique, nil
	}
}

// fanout delivers one event to several destinations.
//
// It carries no retry of its own: retry lives inside each destination, and
// retrying the composite would re-deliver to the destinations that succeeded.
//
// @spec NOTIFY-DEST-007, NOTIFY-DEST-008
type fanout []Destination

func (f fanout) Send(ctx context.Context, e utils.Event) error {
	var errs []error
	for _, d := range f {
		if err := d.Send(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (f fanout) Name() string {
	names := make([]string, len(f))
	for i, d := range f {
		names[i] = d.Name()
	}
	return strings.Join(names, ",")
}
