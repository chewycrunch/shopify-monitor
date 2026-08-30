package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// fakeShop serves a catalog of `total` products, paginated the way Shopify does:
// ?limit=N&page=M, newest first, an empty page past the end.
func fakeShop(t *testing.T, total int) *httptest.Server {
	t.Helper()
	srv, _ := fakeShopCounting(t, total)
	return srv
}

// fakeShopCounting is fakeShop plus a record of which pages were requested.
func fakeShopCounting(t *testing.T, total int) (*httptest.Server, *pageLog) {
	t.Helper()
	seen := &pageLog{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Shopify's default when limit is omitted is 30 — the bug this guards.
		limit := 30
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
			limit = v
		}
		page := 1
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
			page = v
		}

		seen.add(page, limit)

		start := (page - 1) * limit
		end := min(start+limit, total)

		products := []map[string]any{}
		for i := start; i < end; i++ {
			products = append(products, map[string]any{
				"id":     i,
				"title":  fmt.Sprintf("Product %d", i),
				"handle": fmt.Sprintf("product-%d", i),
				"variants": []map[string]any{
					{"id": i*10 + 1, "title": "S", "available": true},
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"products": products}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return srv, seen
}

type pageLog struct {
	mu     sync.Mutex
	pages  []int
	limits []int
}

func (l *pageLog) add(page, limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages = append(l.pages, page)
	l.limits = append(l.limits, limit)
}

func (l *pageLog) snapshot() ([]int, []int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]int(nil), l.pages...), append([]int(nil), l.limits...)
}

func TestFetchAllProductsPaginates(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		concurrency int
	}{
		{"empty catalog", 0, 5},
		{"single short page", 10, 5},
		{"exactly one full page", pageSize, 5},
		{"just over one page", pageSize + 1, 5},
		{"several pages", pageSize*3 + 7, 5},
		{"exact multiple of a batch", pageSize * 5, 5},
		{"serial", pageSize*2 + 1, 1},
		{"concurrency wider than catalog", 5, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := fakeShop(t, tt.total)

			got, err := FetchAllProducts(context.Background(), srv.URL,
				func() *http.Client { return srv.Client() }, tt.concurrency, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.total {
				t.Fatalf("got %d products, want %d", len(got), tt.total)
			}

			// Newest-first order must survive the concurrent reassembly, and
			// every product must appear exactly once.
			seen := make(map[int64]bool, len(got))
			for i, p := range got {
				if p.ID != int64(i) {
					t.Fatalf("product at index %d has id %d — order not preserved", i, p.ID)
				}
				if seen[p.ID] {
					t.Fatalf("product %d returned twice", p.ID)
				}
				seen[p.ID] = true
			}
		})
	}
}

// The whole point of passing ?limit=250: without it Shopify serves 30.
func TestFetchAllProductsRequestsMaxPageSize(t *testing.T) {
	total := pageSize + 50
	srv := fakeShop(t, total)

	got, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total {
		t.Fatalf("got %d products, want %d — is limit= being sent?", len(got), total)
	}
}

// Each page must draw its own client so the requests spread across proxies
// instead of hammering one address.
func TestFetchAllProductsRotatesPerPage(t *testing.T) {
	srv := fakeShop(t, pageSize*3+1)

	var mu sync.Mutex
	handedOut := 0

	_, err := FetchAllProducts(context.Background(), srv.URL, func() *http.Client {
		mu.Lock()
		handedOut++
		mu.Unlock()
		return srv.Client()
	}, 4, 0)
	if err != nil {
		t.Fatal(err)
	}

	if handedOut < 4 {
		t.Errorf("client requested %d times; expected one per page fetched", handedOut)
	}
}

func TestFetchAllProductsPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 3, 0)
	if err == nil {
		t.Fatal("expected an error when the store returns 429")
	}
}

// @spec ACQ-DEPTH-001, ACQ-DEPTH-004
func TestFetchAllProductsHonoursMaxProducts(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		maxProducts int
		want        int
	}{
		{"cap below one page truncates", 1000, 200, 200},
		{"cap on a page boundary", 1000, pageSize, pageSize},
		{"cap mid-catalogue", 5000, 600, 600},
		{"cap above the catalogue returns all of it", 300, 5000, 300},
		{"cap equal to the catalogue", 300, 300, 300},
		{"zero means unlimited", 700, 0, 700},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := fakeShop(t, tt.total)

			got, err := FetchAllProducts(context.Background(), srv.URL,
				func() *http.Client { return srv.Client() }, 5, tt.maxProducts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("got %d products, want %d", len(got), tt.want)
			}
		})
	}
}

// A cap makes the page count knowable up front, so the crawl should ask for
// exactly the pages it needs rather than speculating a batch past the end.
//
// @spec ACQ-DEPTH-002
func TestFetchAllProductsDoesNotOverFetchWhenCapped(t *testing.T) {
	srv, seen := fakeShopCounting(t, 100000)

	if _, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 5, 200); err != nil {
		t.Fatal(err)
	}

	pages, _ := seen.snapshot()
	if len(pages) != 1 {
		t.Errorf("requested %d pages (%v) for a 200-product cap; one page covers it", len(pages), pages)
	}
}

// @spec ACQ-PAGE-007
func TestFetchAllProductsUsesOnePageSizeThroughout(t *testing.T) {
	srv, seen := fakeShopCounting(t, pageSize*3+10)

	if _, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 2, 0); err != nil {
		t.Fatal(err)
	}

	_, limits := seen.snapshot()
	for _, l := range limits {
		if l != pageSize {
			t.Fatalf("a page was requested with limit %d; page offsets only line up if every request uses %d", l, pageSize)
		}
	}
}

// Shopify refuses to paginate past 25,000 products, answering with 400. A
// catalogue larger than that has no short page, so without this the crawl walks
// to the wall and fails — permanently, for the largest stores.
//
// @spec ACQ-PAGE-008
func TestFetchAllProductsStopsAtThePaginationCeiling(t *testing.T) {
	const ceiling = 8 // pages beyond this are refused

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
			page = v
		}
		if page > ceiling {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		products := make([]map[string]any, 0, pageSize)
		for i := range pageSize {
			id := (page-1)*pageSize + i
			products = append(products, map[string]any{
				"id": id, "title": "p", "handle": "p",
				"variants": []map[string]any{{"id": id, "title": "S", "available": true}},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"products": products}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 5, 0)
	if err != nil {
		t.Fatalf("hitting the pagination ceiling should end the crawl, not fail it: %v", err)
	}
	if want := ceiling * pageSize; len(got) != want {
		t.Errorf("got %d products, want %d — everything up to the ceiling", len(got), want)
	}
}

// A 400 on the very first page is a malformed request, not a boundary.
//
// @spec ACQ-PAGE-009
func TestFetchAllProductsFailsOnAFirstPageRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	if _, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 5, 0); err == nil {
		t.Fatal("a 400 on page 1 should be an error, not an empty catalogue")
	}
}

// A large catalogue is a hundred requests, and a baseline crawl must be
// complete — so one transient failure must not cost the whole crawl. Each
// attempt draws a fresh client, which in production means a different proxy.
//
// @spec ACQ-FAIL-011
func TestFetchAllProductsRetriesAFailedPage(t *testing.T) {
	var mu sync.Mutex
	failuresLeft := 2

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fail := failuresLeft > 0
		if fail {
			failuresLeft--
		}
		mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"products": []map[string]any{
			{"id": 1, "title": "p", "handle": "p",
				"variants": []map[string]any{{"id": 1, "title": "S", "available": true}}},
		}}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 1, 0)
	if err != nil {
		t.Fatalf("a page that fails twice then succeeds should not fail the crawl: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d products, want 1", len(got))
	}
}

// @spec ACQ-FAIL-012
func TestFetchAllProductsGivesUpAfterRepeatedPageFailures(t *testing.T) {
	var mu sync.Mutex
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	if _, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 1, 0); err == nil {
		t.Fatal("a page failing every attempt should fail the crawl")
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != pageAttempts {
		t.Errorf("page was attempted %d times, want %d", attempts, pageAttempts)
	}
}

// The ceiling is a definite answer — retrying it through other proxies just
// spends requests to be told the same thing.
//
// @spec ACQ-FAIL-013
func TestFetchAllProductsDoesNotRetryThePaginationCeiling(t *testing.T) {
	var mu sync.Mutex
	beyond := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
			page = v
		}
		if page > 1 {
			mu.Lock()
			beyond++
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		products := make([]map[string]any, 0, pageSize)
		for i := range pageSize {
			products = append(products, map[string]any{
				"id": i, "title": "p", "handle": "p",
				"variants": []map[string]any{{"id": i, "title": "S", "available": true}},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"products": products}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	if _, err := FetchAllProducts(context.Background(), srv.URL,
		func() *http.Client { return srv.Client() }, 1, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if beyond != 1 {
		t.Errorf("page past the ceiling was requested %d times, want 1", beyond)
	}
}
