package services

type RingBuffer struct {
	Proxies []string
	start   int
	size    int
}

func NewRingBuffer(proxies []string) *RingBuffer {
	return &RingBuffer{Proxies: proxies, start: 0, size: len(proxies)}
}

func (r *RingBuffer) GetProxy() string {
	if r.size == 0 {
		return ""
	}

	proxy := r.Proxies[r.start]
	r.start = (r.start + 1) % len(r.Proxies)
	return proxy
}
