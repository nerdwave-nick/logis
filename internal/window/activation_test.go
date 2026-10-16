package window

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

// xdg-activation on a fake compositor: a token is asked for the panel's
// surface with the seat and the serial of the last key or button press,
// the one that chose the app.

var withActivation = append(slices.Clone(withSelections),
	wltest.Global{Interface: "xdg_activation_v1", Version: 1})

// tokenShell plays xdg_activation_v1 beside an inputShell: it logs each
// token's requests and answers its commit with tok-<n>.
type tokenShell struct {
	mu     sync.Mutex
	tokens []uint32
	log    []string
}

func (s *tokenShell) request(f *inputShell, cl *wltest.Client, r wltest.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rd := wltest.NewReader(r)
	if r.Object == cl.Bound("xdg_activation_v1") && r.Opcode == 1 {
		s.tokens = append(s.tokens, rd.Uint())
		return
	}
	n := slices.Index(s.tokens, r.Object)
	if n < 0 {
		return
	}
	switch r.Opcode {
	case 0:
		serial, seat := rd.Uint(), rd.Uint()
		s.log = append(s.log, fmt.Sprintf("set_serial %d seat=%v", serial, seat == cl.Bound("wl_seat")))
	case 1:
		s.log = append(s.log, "set_app_id "+rd.String())
	case 2:
		s.log = append(s.log, fmt.Sprintf("set_surface panel=%v", rd.Uint() == f.surfaceID()))
	case 3:
		s.log = append(s.log, "commit")
		_ = cl.Send(r.Object, 0, fmt.Sprintf("tok-%d", n+1))
	case 4:
		s.log = append(s.log, "destroy")
	}
}

func (s *tokenShell) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.log
	s.log = nil
	return l
}

// activationShown is pointerShown with a keyboard too and the activation
// global played by the returned tokenShell.
func activationShown(t *testing.T, globals []wltest.Global) (*inputShell, *Window, *tokenShell) {
	t.Helper()
	f, w, _ := pointerShown(t, globals)
	s := &tokenShell{}
	f.comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		s.request(f, cl, r)
		f.request(cl, r)
		f.fakeShell.request(cl, r) // closes the request's fds
	})
	waitFor(t, w, "the keyboard", func() bool {
		f.fakeShell.mu.Lock()
		defer f.fakeShell.mu.Unlock()
		return f.keyboard != 0
	})
	return f, w, s
}

func (f *inputShell) key(serial, state uint32) {
	f.send(func(f *fakeShell) uint32 { return f.keyboard }, 3, serial, uint32(1), uint32(28), state)
}

// token asks w for a token for appID, once every event sent so far is
// handled, and waits for it.
func token(t *testing.T, w *Window, appID string) string {
	t.Helper()
	if err := w.conn.display.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	var got []string
	w.RequestToken(appID, func(tok string) { got = append(got, tok) })
	waitFor(t, w, "the token", func() bool { return len(got) > 0 })
	if len(got) != 1 {
		t.Fatalf("tokens %q", got)
	}
	return got[0]
}

// destroyed waits until the token is destroyed; the log up to then.
func (s *tokenShell) destroyed(t *testing.T, w *Window) []string {
	t.Helper()
	waitFor(t, w, "the token's destruction", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return slices.Contains(s.log, "destroy")
	})
	return s.take()
}

func TestATokenIsAskedForThePanelWithTheLastKeyPressesSerial(t *testing.T) {
	f, w, s := activationShown(t, withActivation)
	f.key(77, 1)
	f.key(78, 0) // a release chooses nothing
	if got := token(t, w, "forge-anoint"); got != "tok-1" {
		t.Errorf("token %q", got)
	}
	want := []string{"set_serial 77 seat=true", "set_app_id forge-anoint", "set_surface panel=true", "commit", "destroy"}
	if got := s.destroyed(t, w); !slices.Equal(got, want) {
		t.Errorf("%q\nwant %q", got, want)
	}
}

func TestAButtonPressIsTheSerialToo(t *testing.T) {
	f, w, s := activationShown(t, withActivation)
	f.key(77, 1)
	f.sendPointer(3, uint32(88), uint32(1), uint32(0x110), uint32(1)) // button press
	f.sendPointer(3, uint32(89), uint32(2), uint32(0x110), uint32(0)) // its release
	if got := token(t, w, "forge-bless"); got != "tok-1" {
		t.Errorf("token %q", got)
	}
	if got := s.destroyed(t, w); len(got) == 0 || got[0] != "set_serial 88 seat=true" {
		t.Errorf("%q", got)
	}
}

func TestWithoutAPressTheTokenHasNoSerial(t *testing.T) {
	_, w, s := activationShown(t, withActivation)
	token(t, w, "forge-anoint")
	if got := s.destroyed(t, w); slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "set_serial") }) {
		t.Errorf("%q", got)
	}
}

func TestWithoutXdgActivationNoTokenComesAtOnce(t *testing.T) {
	_, w, _ := activationShown(t, withSelections)
	var got []string
	w.RequestToken("forge-anoint", func(tok string) { got = append(got, tok) })
	if !slices.Equal(got, []string{""}) {
		t.Errorf("tokens %q", got)
	}
}
