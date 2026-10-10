package download

import (
	"errors"
	"strings"
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

// The store's UID admission gate must be closed and drained before this cleanup.
var ErrTransfersActive = errors.New("Application work has not exited")

func (m *Manager) PurgeApplication(uid string, remove func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := "app/" + uid + "-e"
	for _, g := range m.all {
		if strings.HasPrefix(g.Resource.Application, prefix) && g.active() {
			return ErrTransfersActive
		}
	}
	if err := remove(); err != nil {
		return err
	}
	for id, g := range m.all {
		if strings.HasPrefix(g.Resource.Application, prefix) {
			if g.file != nil {
				g.file.Close()
			}
			delete(m.current, g.Resource.ID)
			delete(m.all, id)
		}
	}
	for key := range m.upstreams {
		if strings.HasPrefix(key, prefix) {
			delete(m.upstreams, key)
		}
	}
	return nil
}
