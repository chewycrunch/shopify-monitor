package monitor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// refusingNotifier stands in for a destination that has been disabled: it takes
// nothing, ever.
type refusingNotifier struct {
	mu    sync.Mutex
	calls int
}

func (r *refusingNotifier) Send(context.Context, utils.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return errors.New("destination is disabled")
}

func (r *refusingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// A store whose alerts go nowhere is still worth polling: the log is the only
// remaining evidence of what it saw, and a monitor that stopped would stop
// producing that too.
//
// @spec NOTIFY-FAIL-007, NOTIFY-QUEUE-005
func TestWatchingContinuesWhenAlertsCannotBeDelivered(t *testing.T) {
	srv, pages := fakeShopCounting(t, 3)
	notifier := &refusingNotifier{}

	// Seeded as sold out so the first watch crawl has restocks to report, and
	// therefore deliveries to fail.
	m := &Monitor{
		Url:         srv.URL,
		VariantMap:  map[int64]bool{1: false, 11: false, 21: false},
		notify:      notifier,
		proxyBroker: proxy.NewProxyManager(1),
		pageWorkers: 1,
		log:         slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.StartWatching(ctx, 5*time.Millisecond) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		requested, _ := pages.snapshot()
		if notifier.count() > 0 && len(requested) >= 4 {
			cancel()
			<-done
			return
		}
		time.Sleep(2 * time.Millisecond)
	}

	requested, _ := pages.snapshot()
	cancel()
	<-done
	t.Fatalf("after refused deliveries the monitor made %d page requests and %d delivery attempts; it stopped watching",
		len(requested), notifier.count())
}
