package daemon

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// A client's connection as the listener accepts it: non-blocking, so the
// daemon's goroutine never blocks on it unbounded.
func peerPair(t *testing.T) (*streamPeer, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "daemon"), os.NewFile(uintptr(fds[1]), "client")
	t.Cleanup(func() { _ = ours.Close(); _ = theirs.Close() })
	return &streamPeer{ours}, theirs
}

func TestRepliesReachAReadingClientWhole(t *testing.T) {
	p, client := peerPair(t)
	sent := []protocol.Reply{protocol.Welcome(42), protocol.Visible(), protocol.Error("heresy")}
	for _, r := range sent {
		p.send(r)
	}
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, want := range sent {
		var got protocol.Reply
		if err := protocol.ReadFrame(client, &got); err != nil || got != want {
			t.Fatalf("%+v, %v; want %+v", got, err, want)
		}
	}
}

// A client that stops reading stalls the daemon at most writeTimeout per
// reply (Rust's write timeout): once its socket is full, a reply waits that
// long and gives up.
func TestAClientThatStopsReadingStallsASendAtMostTheWriteTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("timing: the write timeout is measured on the wall clock, not under -short")
	}
	p, _ := peerPair(t)
	big := protocol.Error(strings.Repeat("heresy ", 8_000))
	for range 1000 {
		start := time.Now()
		p.send(big)
		if took := time.Since(start); took > 100*time.Millisecond {
			if took < writeTimeout-100*time.Millisecond || took > writeTimeout+time.Second {
				t.Fatalf("a send to a full socket took %v, want about %v", took, writeTimeout)
			}
			return
		}
	}
	t.Fatal("the socket never filled")
}
