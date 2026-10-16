package wlproto

import (
	"syscall"
	"testing"
	"time"
)

// A watched fd (the window's repeat timer) ends a waiting Dispatch, and its
// handler runs on the dispatching goroutine; once unwatched it does neither.
func TestAWatchedFdEndsAWaitAndItsHandlerRuns(t *testing.T) {
	d, _ := connect(t, niriLike)
	defer d.Close()
	globals(t, d)
	for range 5 {
		if err := d.Dispatch(5 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	var p [2]int
	if err := syscall.Pipe2(p[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(p[0]); _ = syscall.Close(p[1]) }()
	ran := 0
	d.Watch(p[0], func() {
		ran++
		var b [8]byte
		_, _ = syscall.Read(p[0], b[:])
	})
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = syscall.Write(p[1], []byte{1})
	}()
	start := time.Now()
	if err := d.Dispatch(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); ran != 1 || waited > 4*time.Second {
		t.Fatalf("handler ran %d times; Dispatch returned after %v", ran, waited)
	}
	d.Unwatch(p[0])
	_, _ = syscall.Write(p[1], []byte{1})
	start = time.Now()
	if err := d.Dispatch(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); ran != 1 || waited < 30*time.Millisecond {
		t.Errorf("an unwatched fd: handler ran %d times, Dispatch returned after %v", ran, waited)
	}
}
