package proxy

import (
	"net/url"
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

func TestStringify(t *testing.T) {
	tests := []struct {
		name  string
		proxy Proxy
		want  string
	}{
		{"no auth", Proxy{Host: "a.com", Port: "8080"}, "http://a.com:8080"},
		{"with auth", Proxy{Host: "b.com", Port: "8080", User: "user", Pass: "pass"}, "http://user:pass@b.com:8080"},
		{"ipv4", Proxy{Host: "45.3.38.37", Port: "3129"}, "http://45.3.38.37:3129"},
		{"missing port", Proxy{Host: "a.com"}, ""},
		{"missing host", Proxy{Port: "8080"}, ""},
		// @ in a password would otherwise terminate the userinfo section early
		// and point the transport at the wrong host entirely.
		{"escapes credentials", Proxy{Host: "c.com", Port: "8080", User: "u@x", Pass: "p:w@rd"}, "http://u%40x:p%3Aw%40rd@c.com:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.proxy.Stringify(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The bug this guards: url.Parse accepts a schemeless "host:port" without error
// but yields an empty Host, so http.Transport dials :0 rather than the proxy.
func TestStringifyParsesToUsableProxyURL(t *testing.T) {
	pm, err := loadString(t, "a.com:8080\nb.com:8080:user:pass\n")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		p, err := pm.GetProxy()
		if err != nil {
			t.Fatal(err)
		}

		u, err := url.Parse(p.Stringify())
		if err != nil {
			t.Fatalf("parse %q: %v", p.Stringify(), err)
		}
		if u.Host == "" {
			t.Errorf("proxy %q parsed to an empty Host — transport would dial :0", p.Stringify())
		}
		if u.Scheme != "http" {
			t.Errorf("proxy %q parsed to scheme %q, want http", p.Stringify(), u.Scheme)
		}
	}

	// Escaped credentials must survive the round trip, or the proxy rejects the
	// connection with a 407 that looks like a bad password.
	awkward := Proxy{Host: "c.com", Port: "1", User: "u@x", Pass: "p:w@rd"}
	u, err := url.Parse(awkward.Stringify())
	if err != nil {
		t.Fatalf("parse %q: %v", awkward.Stringify(), err)
	}
	if got := u.User.Username(); got != "u@x" {
		t.Errorf("username round-tripped as %q, want %q", got, "u@x")
	}
	if got, _ := u.User.Password(); got != "p:w@rd" {
		t.Errorf("password round-tripped as %q, want %q", got, "p:w@rd")
	}
	if u.Host != "c.com:1" {
		t.Errorf("host is %q, want %q", u.Host, "c.com:1")
	}
}
