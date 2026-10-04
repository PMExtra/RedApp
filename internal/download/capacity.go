package download

import (
	"errors"
	"sync"
)

func (m *Manager) readersLocked() int {
	readers := m.httpReaders
	for _, generation := range m.all {
		readers += generation.readers
	}
	return readers
}

// AcquireHTTPReader reserves the same downstream capacity used by release
// readers. The caller owns response cancellation and must release the lease.
func (m *Manager) AcquireHTTPReader() (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("Server is shutting down")
	}
	if m.readersLocked() >= m.maxReaders {
		return nil, ErrReaderLimit
	}
	m.httpReaders++
	var once sync.Once
	return func() { once.Do(func() { m.mu.Lock(); m.httpReaders--; m.mu.Unlock() }) }, nil
}

// AcquireHTTPWriter reserves one global transfer slot. HTTP cache shutdown must
// cancel its work and release leases before Manager.Close waits for all writers.
func (m *Manager) AcquireHTTPWriter() (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("Server is shutting down")
	}
	if m.jobs >= m.maxWriters {
		return nil, ErrWriterLimit
	}
	m.jobs++
	m.wg.Add(1)
	var once sync.Once
	return func() { once.Do(func() { m.mu.Lock(); m.jobs--; m.mu.Unlock(); m.wg.Done() }) }, nil
}

func (m *Manager) MaxArtifactBytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxBytes
}
