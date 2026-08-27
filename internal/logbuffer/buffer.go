package logbuffer

import (
	"strings"
	"sync"
)

const maxPendingBytes = 64 * 1024

// Buffer is a concurrency-safe, line-oriented ring buffer that also
// implements io.Writer for use with log.Logger and io.MultiWriter.
type Buffer struct {
	mu      sync.RWMutex
	max     int
	lines   []string
	pending string
}

func New(maxLines int) *Buffer {
	if maxLines < 1 {
		maxLines = 1
	}
	return &Buffer{max: maxLines, lines: make([]string, 0, maxLines)}
}

func (b *Buffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.pending += string(data)
	parts := strings.Split(b.pending, "\n")
	b.pending = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		b.appendLocked(strings.TrimSuffix(line, "\r"))
	}
	if len(b.pending) > maxPendingBytes {
		b.pending = b.pending[len(b.pending)-maxPendingBytes:]
	}
	return len(data), nil
}

func (b *Buffer) appendLocked(line string) {
	if len(b.lines) == b.max {
		copy(b.lines, b.lines[1:])
		b.lines = b.lines[:b.max-1]
	}
	b.lines = append(b.lines, line)
}

func (b *Buffer) Lines() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]string(nil), b.lines...)
}
