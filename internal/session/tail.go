package session

import "sync"

// tailBuffer keeps the last max bytes written to it, for diagnostics.
type tailBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newTailBuffer(max int) *tailBuffer {
	if max <= 0 {
		max = 64 * 1024
	}
	return &tailBuffer{max: max}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		drop := len(b.data) - b.max
		copy(b.data, b.data[drop:])
		b.data = b.data[:b.max]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}
