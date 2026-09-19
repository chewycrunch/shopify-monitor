package webhook

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// The events held at shutdown are the ones detected most recently, which makes
// them the ones most worth delivering.
//
// @spec NOTIFY-LIFE-002
func TestShutdownDeliversWhatItIsAlreadyHolding(t *testing.T) {
	rec, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		time.Sleep(20 * time.Millisecond)
		ok(w)
	})

	d := testDiscord(t, url, Options{})
	for _, id := range []int64{1001, 1002, 1003} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if rec.count() != 3 {
		t.Errorf("delivered %d of 3 accepted events before shutting down", rec.count())
	}
}

// @spec NOTIFY-LIFE-001
func TestShutdownStopsAcceptingNewEvents(t *testing.T) {
	_, url := webhookServer(t, func(_ int, w http.ResponseWriter) { ok(w) })

	d := testDiscord(t, url, Options{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if err := d.Send(context.Background(), testEvent(utils.Restock)); err == nil {
		t.Error("an event was accepted after shutdown, with nothing left running to deliver it")
	}
}

// An unresponsive webhook must not be able to keep the process alive.
//
// @spec NOTIFY-LIFE-003
func TestShutdownGivesUpOnAnUnresponsiveWebhookAndSaysWhatItDropped(t *testing.T) {
	block := make(chan struct{})
	_, url := webhookServer(t, func(_ int, w http.ResponseWriter) {
		<-block
		ok(w)
	})
	// Registered after the server so it runs before the server's own cleanup:
	// httptest.Server.Close waits for handlers to return.
	t.Cleanup(func() { close(block) })
	capture, logger := captureLogs()

	d := testDiscord(t, url, Options{Logger: logger})
	for _, id := range []int64{1101, 1102, 1103} {
		e := testEvent(utils.Restock)
		e.Variant.ID = id
		if err := d.Send(context.Background(), e); err != nil {
			t.Fatalf("Send(%d): %v", id, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := d.shutdown(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("shutdown = %v, want it to report running out of time", err)
	}

	if elapsed > time.Second {
		t.Errorf("shutdown took %s against an unresponsive webhook; it waited past its deadline", elapsed)
	}

	reports := capture.matching(slog.LevelWarn, "discard")
	reports = append(reports, capture.matching(slog.LevelError, "discard")...)
	if len(reports) == 0 {
		t.Errorf("shutdown discarded undelivered events without reporting how many; logs:\n%s",
			strings.Join(capture.logs, "\n"))
	}
}

// Two stores naming one webhook share its queue and its budget, so shutting the
// registry down has to reach the destination they share.
//
// @spec NOTIFY-LIFE-001, NOTIFY-DEST-003
func TestRegistryShutdownReachesEveryDestination(t *testing.T) {
	r := NewRegistry(Options{})

	dest, err := r.For(urlA)
	if err != nil {
		t.Fatalf("For(urlA): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := dest.Send(context.Background(), testEvent(utils.Restock)); err == nil {
		t.Error("a destination handed out by the registry still accepted events after the registry shut down")
	}
}
