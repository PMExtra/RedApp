package testutil

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
)

// Logs collects the text-format structured logs of the components under test;
// it is safe for concurrent writers.
type Logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *Logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// Logger writes every level, including debug, to l.
func (l *Logs) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Problems returns the warning and error lines.
func (l *Logs) Problems() []string {
	var out []string
	for _, line := range strings.Split(l.String(), "\n") {
		if strings.Contains(line, "level=WARN") || strings.Contains(line, "level=ERROR") {
			out = append(out, line)
		}
	}
	return out
}
