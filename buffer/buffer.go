package buffer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DiskBuffer persists serialized OTLP span batches to disk for store-and-forward delivery.
type DiskBuffer struct {
	mu      sync.Mutex
	dir     string
	maxSize int64
}

// New creates a DiskBuffer rooted at dir with the given capacity.
func New(dir string, maxSizeMB int64) (*DiskBuffer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create buffer dir: %w", err)
	}
	return &DiskBuffer{dir: dir, maxSize: maxSizeMB << 20}, nil
}

// Write atomically persists a serialized span batch to disk.
func (b *DiskBuffer) Write(data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.diskUsage()+int64(len(data)) > b.maxSize {
		return fmt.Errorf("buffer capacity exceeded (max %d MB)", b.maxSize>>20)
	}

	name := fmt.Sprintf("%d.span", time.Now().UnixNano())
	return os.WriteFile(filepath.Join(b.dir, name), data, 0o644)
}

// Drain returns all buffered batches oldest-first. Callers must Delete the
// returned paths after successful delivery.
func (b *DiskBuffer) Drain() (batches [][]byte, paths []string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries, err := filepath.Glob(filepath.Join(b.dir, "*.span"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(entries)

	for _, p := range entries {
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		batches = append(batches, data)
		paths = append(paths, p)
	}
	return batches, paths, nil
}

// Delete removes successfully delivered batch files from disk.
func (b *DiskBuffer) Delete(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// Size returns the current disk usage in bytes.
func (b *DiskBuffer) Size() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.diskUsage()
}

func (b *DiskBuffer) diskUsage() int64 {
	entries, _ := filepath.Glob(filepath.Join(b.dir, "*.span"))
	var total int64
	for _, e := range entries {
		if fi, err := os.Stat(e); err == nil {
			total += fi.Size()
		}
	}
	return total
}
