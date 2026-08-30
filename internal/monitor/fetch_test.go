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

	return srv
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
				func() *http.Client { return srv.Client() }, tt.concurrency)
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
		func() *http.Client { return srv.Client() }, 3)
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
	}, 4)
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
		func() *http.Client { return srv.Client() }, 3)
	if err == nil {
		t.Fatal("expected an error when the store returns 429")
	}
}
