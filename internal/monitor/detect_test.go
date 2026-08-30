package monitor

import (
	"log/slog"
	"testing"

	"github.com/chewycrunch/shopify-monitor/internal/proxy"
	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

func testMonitor(t *testing.T) *Monitor {
	t.Helper()
	return &Monitor{
		Url:         "http://store.test",
		VariantMap:  make(map[int64]bool),
		proxyBroker: proxy.NewProxyManager(1),
		pageWorkers: 1,
		log:         slog.Default(),
	}
}

// catalog builds a one-product catalogue whose variants carry the given
// availability, keyed by variant id.
func catalog(variants map[int64]bool) []utils.Product {
	p := utils.Product{ID: 1, Title: "Test Sneaker", Handle: "test-sneaker"}
	for id, avail := range variants {
		p.Variants = append(p.Variants, utils.Variant{ID: id, Title: "Size 10", Available: avail})
	}
	return []utils.Product{p}
}

// @spec DET-BASE-001, DET-BASE-002
func TestRecordBaselineIsSilentAndCounts(t *testing.T) {
	m := testMonitor(t)

	got := m.recordBaseline(catalog(map[int64]bool{1: true, 2: false, 3: true}))

	if got != 3 {
		t.Errorf("recorded %d variants, want 3", got)
	}
	for id, want := range map[int64]bool{1: true, 2: false, 3: true} {
		if have, ok := m.VariantMap[id]; !ok || have != want {
			t.Errorf("variant %d recorded as (%v, present=%v), want %v", id, have, ok, want)
		}
	}
}

// @spec DET-EVENT-001, DET-EVENT-002, DET-EVENT-003, DET-EVENT-004, DET-EVENT-005
func TestDetectChangesClassifies(t *testing.T) {
	tests := []struct {
		name     string
		recorded map[int64]bool
		observed bool
		want     EventKind
	}{
		{"unseen and available reports a new variant", nil, true, NewVariant},
		{"unseen and unavailable is silent", nil, false, 0},
		{"unavailable becoming available reports a restock", map[int64]bool{7: false}, true, Restock},
		{"available becoming unavailable is silent", map[int64]bool{7: true}, false, 0},
		{"available staying available is silent", map[int64]bool{7: true}, true, 0},
		{"unavailable staying unavailable is silent", map[int64]bool{7: false}, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testMonitor(t)
			for id, avail := range tt.recorded {
				m.VariantMap[id] = avail
			}

			events := m.detectChanges(catalog(map[int64]bool{7: tt.observed}))

			if tt.want == 0 {
				if len(events) != 0 {
					t.Fatalf("got %d events, want none: %+v", len(events), events)
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d events, want exactly 1", len(events))
			}
			if events[0].Kind != tt.want {
				t.Errorf("event kind = %v, want %v", events[0].Kind, tt.want)
			}
		})
	}
}

// The bug this guards: writing the record only on the branches that report an
// event leaves a sell-out unrecorded, so the following restock compares equal
// and is never reported. A variant available when first seen would be
// permanently undetectable after its first sell-out.
//
// @spec DET-RECORD-002, DET-EVENT-003
func TestDetectChangesRecordsSellOutSoTheNextRestockIsSeen(t *testing.T) {
	m := testMonitor(t)
	m.recordBaseline(catalog(map[int64]bool{7: true}))

	if events := m.detectChanges(catalog(map[int64]bool{7: false})); len(events) != 0 {
		t.Fatalf("selling out should report nothing, got %+v", events)
	}
	if m.VariantMap[7] {
		t.Fatal("variant still recorded as available after selling out")
	}

	events := m.detectChanges(catalog(map[int64]bool{7: true}))
	if len(events) != 1 || events[0].Kind != Restock {
		t.Fatalf("restock after a sell-out was not reported, got %+v", events)
	}
}

// @spec DET-RECORD-003, DET-RECORD-004
func TestDetectChangesIgnoresVariantsAbsentFromTheCrawl(t *testing.T) {
	m := testMonitor(t)
	m.recordBaseline(catalog(map[int64]bool{7: true, 8: false}))

	// A crawl that missed the page holding variant 8.
	events := m.detectChanges(catalog(map[int64]bool{7: true}))

	if len(events) != 0 {
		t.Fatalf("got %d events for an unchanged partial crawl: %+v", len(events), events)
	}
	if have, ok := m.VariantMap[8]; !ok || have != false {
		t.Errorf("absent variant 8 recorded as (%v, present=%v); absence must not change the record", have, ok)
	}
}

// @spec DET-EVENT-006
func TestEventsIdentifyTheProductAndVariant(t *testing.T) {
	m := testMonitor(t)
	m.VariantMap[7] = false

	events := m.detectChanges(catalog(map[int64]bool{7: true}))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	e := events[0]
	if e.Product.Title != "Test Sneaker" || e.Product.Handle != "test-sneaker" {
		t.Errorf("event product = %q/%q, want the observed product", e.Product.Title, e.Product.Handle)
	}
	if e.Variant.ID != 7 || e.Variant.Title != "Size 10" {
		t.Errorf("event variant = %d/%q, want the observed variant", e.Variant.ID, e.Variant.Title)
	}
}
