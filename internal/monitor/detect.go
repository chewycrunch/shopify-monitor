package monitor

import "github.com/chewycrunch/shopify-monitor/internal/utils"

// EventKind is the sort of change a watch crawl found.
type EventKind int

const (
	// NewVariant is a variant observed for the first time, already available.
	NewVariant EventKind = iota + 1
	// Restock is a known variant that was unavailable and is now available.
	Restock
)

// Event is a reportable change, carrying what a notification needs to say.
type Event struct {
	Kind    EventKind
	Product utils.Product
	Variant utils.Variant
}

// recordBaseline establishes the store's availability record from its first
// crawl and returns the number of variants recorded.
//
// Silent by design: every variant is being seen for the first time, so treating
// first sightings as events here would report the whole catalogue at startup.
//
// @spec DET-BASE-001, DET-BASE-002
func (m *Monitor) recordBaseline(products []utils.Product) int {
	recorded := 0

	for _, product := range products {
		for _, variant := range product.Variants {
			m.VariantMap[variant.ID] = variant.Available
			recorded++
		}
	}

	return recorded
}

// detectChanges records the availability of every variant observed and returns
// the changes worth reporting.
//
// Recording and reporting are deliberately separate. The record is written for
// every observation, including the ones that report nothing — a sell-out left
// unrecorded would leave a stale "available" entry, and the restock that
// follows would compare equal and never be reported.
//
// Variants absent from products are not observations. Their entries are left
// alone, because a crawl that skipped a failed page is indistinguishable here
// from a catalogue that no longer lists them.
//
// @spec DET-RECORD-002, DET-RECORD-003, DET-RECORD-004, DET-EVENT-001, DET-EVENT-002, DET-EVENT-003, DET-EVENT-004, DET-EVENT-005, DET-EVENT-006
func (m *Monitor) detectChanges(products []utils.Product) []Event {
	var events []Event

	for _, product := range products {
		for _, variant := range product.Variants {
			previous, recorded := m.VariantMap[variant.ID]
			m.VariantMap[variant.ID] = variant.Available

			// Both reported cases mean the same thing to an operator: buyable
			// now, not buyable before. A variant that appears already sold out
			// is a listing rather than an event, and a sell-out is recorded
			// only so the next restock can be recognised.
			switch {
			case !recorded && variant.Available:
				events = append(events, Event{Kind: NewVariant, Product: product, Variant: variant})
			case recorded && !previous && variant.Available:
				events = append(events, Event{Kind: Restock, Product: product, Variant: variant})
			}
		}
	}

	return events
}
