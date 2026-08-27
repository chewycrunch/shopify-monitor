package proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type Proxy struct {
	Host string
	Port string
	User string
	Pass string
}

type ProxyManager struct {
	proxies []Proxy
	start   int
	size    int
}

func NewProxyManager(initialCapacity int) *ProxyManager {
	return &ProxyManager{
		proxies: make([]Proxy, 0, initialCapacity),
		start:   0,
		size:    0,
	}
}

func (pm *ProxyManager) LoadProxiesFromFile(file *os.File) error {
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		proxyStr := strings.TrimSpace(scanner.Text())

		// Blank lines and comments are how people annotate these files; a
		// trailing newline alone produces one. Neither is a proxy.
		if proxyStr == "" || strings.HasPrefix(proxyStr, "#") {
			continue
		}

		proxy, err := newProxy(proxyStr)
		if err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		pm.addProxy(proxy)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func (pm *ProxyManager) addProxy(proxy Proxy) {
	pm.proxies = append(pm.proxies, proxy)
	pm.size++
}

func (pm *ProxyManager) GetProxy() (Proxy, error) {
	if pm.size == 0 {
		return Proxy{}, fmt.Errorf("no proxies available")
	}

	proxy := pm.proxies[pm.start]
	pm.start = (pm.start + 1) % len(pm.proxies)
	return proxy, nil
}

func newProxy(proxyStr string) (Proxy, error) {
	spl := strings.Split(proxyStr, ":")

	if len(spl) != 2 && len(spl) != 4 {
		return Proxy{}, fmt.Errorf("malformed proxy %q: want host:port or host:port:user:pass", proxyStr)
	}

	p := Proxy{
		Host: spl[0],
		Port: spl[1],
	}

	if len(spl) == 4 {
		p.User = spl[2]
		p.Pass = spl[3]
	}

	return p, nil
}

// Stringify renders the proxy as a URL for http.Transport.
//
// The scheme is not optional. url.Parse reads a bare "host:port" as scheme
// "host" with an empty Host and returns no error, so a schemeless proxy is not
// rejected anywhere — it silently becomes a transport that dials :0 and fails
// every request with "can't assign requested address".
//
// Built through net/url rather than fmt so credentials containing @ or : are
// escaped instead of corrupting the URL, and so IPv6 hosts get their brackets.
func (p *Proxy) Stringify() string {
	if p.Host == "" || p.Port == "" {
		return ""
	}

	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(p.Host, p.Port),
	}

	if p.User != "" || p.Pass != "" {
		u.User = url.UserPassword(p.User, p.Pass)
	}

	return u.String()
}
