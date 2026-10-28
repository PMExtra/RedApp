package httpcache

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// A source listing is shared by every waiter, so a waiter leaving does not
// cancel it, but closing the service does.
func TestIndexFlightEndsWithTheServiceNotWithAWaiter(t *testing.T) {
	entered := make(chan struct{}, 4)
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
	}), 60)
	waiter, leave := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := f.s.Index(waiter, f.entry, "", 10)
		first <- err
	}()
	<-entered
	second := make(chan error, 1)
	go func() {
		_, err := f.s.Index(context.Background(), f.entry, "", 10)
		second <- err
	}()
	leave()
	if err := <-first; err == nil {
		t.Fatal("a leaving waiter got a listing")
	}
	select {
	case err := <-second:
		t.Fatal("the shared fetch ended with its first waiter", err)
	case <-time.After(100 * time.Millisecond):
	}
	closed := make(chan struct{})
	go func() {
		f.s.Close()
		close(closed)
	}()
	select {
	case err := <-second:
		if err == nil {
			t.Fatal("a listing after the service closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the service did not end the shared fetch")
	}
	<-closed
	select {
	case <-entered:
		t.Fatal("the source was asked twice")
	default:
	}
}
