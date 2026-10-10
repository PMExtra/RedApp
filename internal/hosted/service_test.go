package hosted

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

type testBudget struct {
	writers chan struct{}
	readers chan struct{}
	size    int64
}

func (b *testBudget) AcquireHTTPWriter() (func(), error) {
	select {
	case b.writers <- struct{}{}:
		return func() { <-b.writers }, nil
	default:
		return nil, download.ErrWriterLimit
	}
}
func (b *testBudget) AcquireHTTPReader() (func(), error) {
	select {
	case b.readers <- struct{}{}:
		return func() { <-b.readers }, nil
	default:
		return nil, download.ErrReaderLimit
	}
}
func (b *testBudget) MaxArtifactBytes() int64 { return b.size }
func setup(t *testing.T) (*Service, application.Entry, *testBudget) {
	t.Helper()
	dir := t.TempDir()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	v, e := db.CreateVendor(store.VendorInput{ID: "acme", Name: store.LocalizedText{En: "Acme", ZhCN: "示例"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	a, e := db.CreateApplication(v.ID, store.ApplicationInput{ID: "files", Name: store.LocalizedText{En: "Files", ZhCN: "文件"}, Provider: application.Hosted, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	budget := &testBudget{make(chan struct{}, 1), make(chan struct{}, 1), 16}
	s, e := New(dir, db, budget)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s, application.Entry{UID: a.UID, Provider: a.Provider, Descriptor: application.Descriptor{ID: a.Key}, Revision: a.Revision, VendorRevision: v.Revision}, budget
}
func transferID(t *testing.T) string {
	t.Helper()
	id, e := identity.NewUID()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func body(value string) func(context.Context) (io.ReadCloser, int64, error) {
	return func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader(value)), int64(len(value)), nil
	}
}
func TestHostedAtomicCancellationLimitsAndConflict(t *testing.T) {
	s, entry, budget := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer writer.Close()
	opened := make(chan struct{})
	done := make(chan error, 1)
	id := transferID(t)
	go func() {
		_, err := s.Put(ctx, entry, "nested/file.bin", "", id, func(context.Context) (io.ReadCloser, int64, error) { close(opened); return reader, -1, nil })
		done <- err
	}()
	<-opened
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.HostedFile(entry.UID, "nested/file.bin"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial file visible", err)
	}
	if _, err := s.Put(ctx, entry, "other", "", transferID(t), body("x")); !errors.Is(err, download.ErrWriterLimit) {
		t.Fatal("writer limit", err)
	}
	p, ok := s.Progress(entry.UID, id)
	if !ok || p.Path != "nested/file.bin" {
		t.Fatal(p, ok)
	}
	if s.Cancel("wrong-app", id) || !s.Cancel(entry.UID, id) {
		t.Fatal("cancel scope")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel committed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock reader")
	}
	if _, ok := s.Progress(entry.UID, id); ok {
		t.Fatal("completed transfer retained")
	}
	assertNoFiles := func() {
		t.Helper()
		files, err := os.ReadDir(s.dir)
		if err != nil || len(files) != 0 {
			t.Fatal("partial/orphan survived", files, err)
		}
	}
	assertNoFiles()
	if _, err := s.Put(context.Background(), entry, "big", "", transferID(t), body(strings.Repeat("x", 17))); !errors.Is(err, download.ErrArtifactLimit) {
		t.Fatal(err)
	}
	assertNoFiles()
	if _, err := s.Put(context.Background(), entry, "unknown", "", transferID(t), func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader(strings.Repeat("x", 17))), -1, nil
	}); !errors.Is(err, download.ErrArtifactLimit) {
		t.Fatal("unknown length", err)
	}
	assertNoFiles()
	first, err := s.Put(context.Background(), entry, "nested/file.bin", "", transferID(t), body("old"))
	if err != nil {
		t.Fatal(err)
	}
	old, _, release, err := s.Open(entry.UID, first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.Open(entry.UID, first.Path); !errors.Is(err, download.ErrReaderLimit) {
		t.Fatal("reader limit", err)
	}
	if _, err = s.Put(context.Background(), entry, first.Path, "", transferID(t), body("unasked")); !errors.Is(err, store.ErrHostedFileChanged) {
		t.Fatal("overwrite without CAS", err)
	}
	next, err := s.Put(context.Background(), entry, first.Path, first.ID, transferID(t), body("new"))
	if err != nil || next.ID == first.ID {
		t.Fatal(next, err)
	}
	oldBytes, _ := io.ReadAll(old)
	old.Close()
	release()
	if string(oldBytes) != "old" {
		t.Fatal("existing reader disrupted", string(oldBytes))
	}
	if err = s.Delete(entry.UID, first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("stale delete removed replacement", err)
	}
	f, row, release, err := s.Open(entry.UID, first.Path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	release()
	if string(data) != "new" || row.ID != next.ID {
		t.Fatal(row, string(data))
	}
	s.Close()
	restarted, err := New(filepath.Dir(filepath.Dir(s.dir)), s.db, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err = restarted.db.HostedFile(entry.UID, first.Path); err != nil {
		t.Fatal("restart lost permanent resource", err)
	}
	if err = restarted.Delete(entry.UID, next.ID); err != nil {
		t.Fatal(err)
	}
	assertNoFiles()
}
func TestHostedRejectsChangedAppAndUnsafePaths(t *testing.T) {
	s, entry, _ := setup(t)
	for _, path := range []string{"../file", "/file", "a//b", "a/../b", "a%2fb", "a?token=secret", "a\\b"} {
		if _, err := s.Put(context.Background(), entry, path, "", transferID(t), body("x")); !errors.Is(err, store.ErrInvalidDirectory) {
			t.Fatal(path, err)
		}
	}
	a, _ := s.db.Application(entry.Descriptor.ID)
	_, err := s.db.UpdateApplication(a.Key, a.Revision, store.ApplicationChanges{Name: a.Name, Description: a.Description, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(context.Background(), entry, "file", "", transferID(t), body("complete")); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale app commit", err)
	}
	files, _ := os.ReadDir(s.dir)
	if len(files) != 0 {
		t.Fatal("failed publication orphan", files)
	}
}

func TestHostedDeleteFencesInflightReplacementAndPreservesOpenedReader(t *testing.T) {
	s, entry, _ := setup(t)
	first, err := s.Put(context.Background(), entry, "file.bin", "", transferID(t), body("original"))
	if err != nil {
		t.Fatal(err)
	}
	reader, _, release, err := s.Open(entry.UID, first.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer release()
	pipe, writer := io.Pipe()
	defer writer.Close()
	opened := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := s.Put(context.Background(), entry, first.Path, first.ID, transferID(t), func(context.Context) (io.ReadCloser, int64, error) { close(opened); return pipe, -1, nil })
		done <- e
	}()
	<-opened
	if _, err = writer.Write([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(entry.UID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.HostedFile(entry.UID, first.Path); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("delete left public row", err)
	}
	writer.Close()
	if err = <-done; !errors.Is(err, store.ErrHostedFileChanged) {
		t.Fatal("replacement resurrected deleted file", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "original" {
		t.Fatal("delete interrupted an existing reader", string(data), err)
	}
	files, err := os.ReadDir(s.dir)
	if err != nil || len(files) != 0 {
		t.Fatal("replacement left unreferenced files", files, err)
	}
}
