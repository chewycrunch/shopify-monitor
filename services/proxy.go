package services

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Proxy struct {
	Host string
	Port string
	User string
	Pass string
}

type ProxyBroker struct {
	proxies []Proxy
	start   int
	size    int
}

func NewProxyManager(initialCapacity int) *ProxyBroker {
	return &ProxyBroker{
		proxies: make([]Proxy, 0, initialCapacity),
		start:   0,
		size:    0,
	}
}

func (pm *ProxyBroker) LoadProxiesFromFile(file *os.File) error {
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxyStr := scanner.Text()
		proxy := newProxy(proxyStr)
		pm.addProxy(proxy)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func (pm *ProxyBroker) addProxy(proxy Proxy) {
	pm.proxies = append(pm.proxies, proxy)
	pm.size++
}

func (pm *ProxyBroker) GetProxy() (Proxy, error) {
	if pm.size == 0 {
		return Proxy{}, fmt.Errorf("no proxies available")
	}

	proxy := pm.proxies[pm.start]
	pm.start = (pm.start + 1) % len(pm.proxies)
	return proxy, nil
}

func newProxy(proxyStr string) Proxy {
	spl := strings.Split(proxyStr, ":")

	p := Proxy{
		Host: spl[0],
		Port: spl[1],
	}

	if len(spl) == 4 {
		p.User = spl[2]
		p.Pass = spl[3]
	}

	return p
}

func (p *Proxy) Stringify() string {
	if p.Host == "" || p.Port == "" {
		return ""
	}

	if p.User == "" && p.Pass == "" {
		return fmt.Sprintf("%s:%s", p.Host, p.Port)
	}

	return fmt.Sprintf("http://%s:%s@%s:%s", p.User, p.Pass, p.Host, p.Port)

}
