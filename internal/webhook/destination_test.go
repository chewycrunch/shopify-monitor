package webhook

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// Two real-shaped Discord webhook URLs. The token halves are what must never
// reach a log line.
const (
	urlA   = "https://discord.com/api/webhooks/1542630725822578870/hlvZqMsPqocrdi3NpNhn0xB5hS5sgN"
	tokenA = "hlvZqMsPqocrdi3NpNhn0xB5hS5sgN"
	idA    = "1542630725822578870"

	urlB   = "https://discord.com/api/webhooks/1543000000000000001/p4tw4va50rdlIfS27CclaxevHhL837"
	tokenB = "p4tw4va50rdlIfS27CclaxevHhL837"
)

func testEvent(kind utils.EventKind) utils.Event {
	return utils.Event{
		Kind:  kind,
		Store: "https://kith.com",
		Product: utils.Product{
			ID: 1, Title: "Test Sneaker", Handle: "test-sneaker",
			Images: []utils.Image{{ID: 9, Src: "https://cdn.test/first.jpg"}, {ID: 10, Src: "https://cdn.test/second.jpg"}},
		},
		Variant:    utils.Variant{ID: 7, Title: "Size 10", Available: true},
		DetectedAt: time.Date(2026, 9, 4, 12, 30, 15, 0, time.UTC),
	}
}

// recordingDest is a Destination that counts what it accepted and can refuse.
type recordingDest struct {
	name string
	err  error

	mu  sync.Mutex
	got []utils.Event
}

func (r *recordingDest) Send(_ context.Context, e utils.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.got = append(r.got, e)
	return nil
}

func (r *recordingDest) Name() string { return r.name }

func (r *recordingDest) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// @spec NOTIFY-DEST-001, NOTIFY-DEST-002, NOTIFY-DISCORD-001
func TestForRecognisesDestinationsFromTheURLAlone(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		recognised bool
	}{
		{"a discord webhook", urlA, true},
		{"the legacy discordapp host", "https://discordapp.com/api/webhooks/123/token", true},
		{"discord over http", "http://discord.com/api/webhooks/123/token", true},
		{"a discord url that is not a webhook", "https://discord.com/channels/123/456", false},
		{"another vendor's webhook", "https://hooks.slack.com/services/T0/B0/xxx", false},
		{"a store url pasted into the webhook column", "https://kith.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(Options{})

			dest, err := r.For(tc.url)
			if tc.recognised {
				if err != nil {
					t.Fatalf("For(%q) = %v, want a destination", tc.url, err)
				}
				if dest == nil {
					t.Fatalf("For(%q) returned no destination and no error", tc.url)
				}
				return
			}
			if !errors.Is(err, ErrUnrecognised) {
				t.Fatalf("For(%q) = %v, want ErrUnrecognised", tc.url, err)
			}
		})
	}
}

// Two stores naming one webhook are one consumer of that webhook's budget.
// Separate instances would each pace correctly and collectively exceed it.
//
// @spec NOTIFY-DEST-003, NOTIFY-DELIVER-003
func TestForReturnsOneDestinationPerWebhookURL(t *testing.T) {
	r := NewRegistry(Options{})

	first, err := r.For(urlA)
	if err != nil {
		t.Fatalf("For(urlA): %v", err)
	}
	again, err := r.For(urlA)
	if err != nil {
		t.Fatalf("For(urlA) second call: %v", err)
	}
	other, err := r.For(urlB)
	if err != nil {
		t.Fatalf("For(urlB): %v", err)
	}

	if first != again {
		t.Error("the same webhook URL produced two destinations; its budget would be spent twice over")
	}
	if first == other {
		t.Error("two different webhook URLs collapsed to one destination")
	}
}

// A webhook URL's token grants the right to post to it, so a log line quoting
// one hands that right to anyone who can read the logs.
//
// @spec NOTIFY-DEST-004, NOTIFY-DISCORD-005
func TestNameIdentifiesTheWebhookWithoutItsToken(t *testing.T) {
	r := NewRegistry(Options{})

	a, err := r.For(urlA)
	if err != nil {
		t.Fatalf("For(urlA): %v", err)
	}
	b, err := r.For(urlB)
	if err != nil {
		t.Fatalf("For(urlB): %v", err)
	}

	if strings.Contains(a.Name(), tokenA) {
		t.Errorf("Name() = %q, which carries the webhook token", a.Name())
	}
	if !strings.Contains(a.Name(), idA) {
		t.Errorf("Name() = %q, want it to carry the webhook id %s", a.Name(), idA)
	}
	if a.Name() == b.Name() {
		t.Errorf("both webhooks are named %q; they cannot be told apart in a log", a.Name())
	}
	if strings.Contains(b.Name(), tokenB) {
		t.Errorf("Name() = %q, which carries the webhook token", b.Name())
	}
}

// @spec NOTIFY-DEST-005
func TestFanoutDeliversToEveryDestination(t *testing.T) {
	one, two, three := &recordingDest{name: "one"}, &recordingDest{name: "two"}, &recordingDest{name: "three"}

	f, err := Fanout(one, two, three)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if err := f.Send(context.Background(), testEvent(utils.Restock)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, d := range []*recordingDest{one, two, three} {
		if d.count() != 1 {
			t.Errorf("destination %q accepted %d events, want 1", d.name, d.count())
		}
	}
}

// An operator who lists a webhook twice wants one alert, not two. Identical
// URLs resolve through the registry to one instance, so duplicates are
// detectable by identity.
//
// @spec NOTIFY-DEST-006
func TestFanoutDeliversOnceToADuplicatedDestination(t *testing.T) {
	only := &recordingDest{name: "only"}

	f, err := Fanout(only, only)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if err := f.Send(context.Background(), testEvent(utils.Restock)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if only.count() != 1 {
		t.Errorf("a webhook named twice accepted %d events, want 1", only.count())
	}
}

// One dead webhook must not silence the healthy ones beside it, and the
// composite must not retry — retrying it would re-deliver to the destinations
// that already accepted the event.
//
// @spec NOTIFY-DEST-007, NOTIFY-DEST-008
func TestFanoutContinuesPastARefusalWithoutRedelivering(t *testing.T) {
	broken := &recordingDest{name: "broken", err: errors.New("refused")}
	healthy := &recordingDest{name: "healthy"}

	f, err := Fanout(broken, healthy)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}

	sendErr := f.Send(context.Background(), testEvent(utils.Restock))

	if healthy.count() != 1 {
		t.Errorf("healthy destination accepted %d events, want exactly 1", healthy.count())
	}
	if sendErr == nil {
		t.Error("Send reported success though one destination refused")
	}
	if !strings.Contains(sendErr.Error(), "broken") {
		t.Errorf("Send error = %v, want it to name the destination that refused", sendErr)
	}
}

// @spec NOTIFY-DEST-005
func TestFanoutOfOneIsThatDestination(t *testing.T) {
	only := &recordingDest{name: "only"}

	f, err := Fanout(only)
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if f != Destination(only) {
		t.Error("composing one destination wrapped it instead of returning it")
	}
}

// A store with nowhere to report is refused when the stores file is read, so
// reaching here without a destination is a defect, not a configuration.
func TestFanoutOfNoneIsRefused(t *testing.T) {
	if _, err := Fanout(); !errors.Is(err, ErrNoDestinations) {
		t.Fatalf("Fanout() = %v, want ErrNoDestinations", err)
	}
}
