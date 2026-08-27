package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func loadString(t *testing.T, contents string) (*ProxyManager, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pm := NewProxyManager(4)
	return pm, pm.LoadProxiesFromFile(f)
}

func TestLoadProxiesFromFile(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     int
	}{
		{"no trailing newline", "a.com:8080\nb.com:8080", 2},
		{"one trailing newline", "a.com:8080\nb.com:8080\n", 2},
		{"two trailing newlines", "a.com:8080\nb.com:8080\n\n", 2},
		{"many trailing newlines", "a.com:8080\n\n\n\n", 1},
		{"blank line in middle", "a.com:8080\n\nb.com:8080\n", 2},
		{"windows line endings", "a.com:8080\r\nb.com:8080\r\n", 2},
		{"comments and blanks", "# header\n\na.com:8080\n# note\nb.com:8080\n", 2},
		{"indented entry", "  a.com:8080  \n", 1},
		{"with basic auth", "a.com:8080:user:pass\n", 1},
		{"empty file", "", 0},
		{"only newlines", "\n\n\n", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, err := loadString(t, tt.contents)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pm.size != tt.want {
				t.Errorf("got %d proxies, want %d", pm.size, tt.want)
			}
		})
	}
}

func TestLoadProxiesRejectsMalformed(t *testing.T) {
	if _, err := loadString(t, "a.com:8080\nnotaproxy\n"); err == nil {
		t.Fatal("expected an error for a malformed line, got nil")
	}
}

func TestStringifyRoundTrip(t *testing.T) {
	pm, err := loadString(t, "a.com:8080\nb.com:8080:user:pass\n")
	if err != nil {
		t.Fatal(err)
	}

	first, err := pm.GetProxy()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := first.Stringify(), "a.com:8080"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	second, err := pm.GetProxy()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.Stringify(), "http://user:pass@b.com:8080"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
