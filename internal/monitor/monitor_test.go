package monitor

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already clean", "https://kith.com", "https://kith.com"},
		{"trailing slash", "https://rsgoffroad.com/", "https://rsgoffroad.com"},
		{"several trailing slashes", "https://kith.com///", "https://kith.com"},
		{"surrounding whitespace", "  https://kith.com  ", "https://kith.com"},
		{"whitespace and slash", " https://kith.com/ ", "https://kith.com"},
		{"path preserved", "https://kith.com/collections/new/", "https://kith.com/collections/new"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBaseURL(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
