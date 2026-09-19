package monitor

import (
	"time"

	"github.com/chewycrunch/shopify-monitor/internal/utils"
)

// Event and its kinds are defined in utils so that the segment delivering an
// event and the segment producing it share the type without importing each
// other. These aliases keep the classification code reading in its own terms.
type (
	EventKind = utils.EventKind
	Event     = utils.Event
)

const (
	NewVariant = utils.NewVariant
	Restock    = utils.Restock
)

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

	// Stamped here rather than at delivery: a queued alert may go out long
	// after the stock moved, and the operator is acting on when it moved.
	now := time.Now().UTC()

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
				events = append(events, Event{Kind: NewVariant, Product: product, Variant: variant, Store: m.Url, DetectedAt: now})
			case recorded && !previous && variant.Available:
				events = append(events, Event{Kind: Restock, Product: product, Variant: variant, Store: m.Url, DetectedAt: now})
			}
		}
	}

	return events
}
