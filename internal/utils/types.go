package utils

import "time"

type Variant struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Available bool   `json:"available"`
}

type Product struct {
	ID       int64     `json:"id"`
	Title    string    `json:"title"`
	Handle   string    `json:"handle"`
	Variants []Variant `json:"variants"`
	Images   []Image   `json:"images"`
}

type Image struct {
	ID  int64  `json:"id"`
	Src string `json:"src"`
}

type ProductsResponse struct {
	Products []Product `json:"products"`
}

// EventKind is the sort of change a watch crawl found.
type EventKind int

const (
	// NewVariant is a variant observed for the first time, already available.
	NewVariant EventKind = iota + 1
	// Restock is a known variant that was unavailable and is now available.
	Restock
)

// Event is a reportable change, carrying what a notification needs to say.
//
// It lives here rather than beside the code that classifies it because both the
// segment that produces it and the segment that delivers it need the type, and
// neither should have to import the other.
type Event struct {
	Kind    EventKind
	Product Product
	Variant Variant

	// Store is the normalized base URL of the catalogue the change was seen
	// in. A destination shared by several stores has no single store to infer,
	// and a product link cannot be built without it.
	Store string

	// DetectedAt is when the change was classified, not when it was delivered.
	// A queued event may be delivered long after the stock actually moved.
	DetectedAt time.Time
}
