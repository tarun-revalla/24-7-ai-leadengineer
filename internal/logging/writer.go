package logging

import (
	"fmt"
	"os"
	"sync"
)

// ConsoleWriter writes log messages to console with optional coloring.
type ConsoleWriter struct {
	mu sync.Mutex
}

// NewConsoleWriter creates a new console writer.
func NewConsoleWriter() *ConsoleWriter {
	return &ConsoleWriter{}
}

// Write writes data to console.
func (w *ConsoleWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return fmt.Fprint(os.Stderr, string(p))
}

// Sync is a no-op for console.
func (w *ConsoleWriter) Sync() error {
	return nil
}
