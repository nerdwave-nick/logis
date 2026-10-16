package e2e

import (
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Offered is the MIME types every source offers, the preferred one not
// first, so a test can check which one the client chose.
var Offered = []string{"text/plain", "STRING", "text/plain;charset=utf-8", "UTF8_STRING", "TEXT"}

// SelectionOwner is a test-only selection owner: a client of the test
// compositor that owns the clipboard and the primary selection through
// wlr-data-control (both test compositors offer it; it needs no keyboard
// focus or serial) and writes their text to whoever asks, on a goroutine
// of its own, until stopped. It replaces wl-copy: nothing to install on
// the CI runner, and its connection goes through ConnectVerified, so it
// can never touch the live clipboard.
type SelectionOwner struct {
	mu       sync.Mutex
	requests []string // guarded by mu
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

// OwnSelection takes c's clipboard and primary selection with the given
// texts ("" leaves that selection alone); t's cleanup gives them up.
func OwnSelection(t testing.TB, c *Compositor, clipboard, primary string) *SelectionOwner {
	t.Helper()
	o := &SelectionOwner{stop: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go o.serve(c.Socket(), c.PID(), clipboard, primary, started)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Stop)
	return o
}

// serve owns its connection: made, used and closed on this goroutine.
func (o *SelectionOwner) serve(socket string, pid int, clipboard, primary string, started chan<- error) {
	defer close(o.done)
	d, err := ConnectVerified(socket, pid)
	if err != nil {
		started <- err
		return
	}
	defer d.Close()
	globals := map[string]global{}
	registry := d.Registry()
	registry.OnGlobal = func(name uint32, iface string, version uint32) { globals[iface] = global{name, version} }
	if err := d.Roundtrip(); err != nil {
		started <- err
		return
	}
	seatG, hasSeat := globals[wlproto.WlSeatInterface.Name]
	managerG, hasManager := globals[wlproto.ZwlrDataControlManagerV1Interface.Name]
	switch {
	case !hasSeat:
		started <- errors.New("the test compositor offers no seat")
		return
	case !hasManager:
		started <- errors.New("the test compositor does not offer zwlr_data_control_manager_v1")
		return
	case primary != "" && managerG.version < 2:
		started <- errors.New("zwlr_data_control_manager_v1 v2 is needed for the primary selection")
		return
	}
	seat, _ := registry.Bind(seatG.name, wlproto.WlSeatInterface, 1).(*wlproto.WlSeat)
	manager, _ := registry.Bind(managerG.name, wlproto.ZwlrDataControlManagerV1Interface, min(managerG.version, 2)).(*wlproto.ZwlrDataControlManagerV1)
	device := manager.GetDataDevice(seat)
	source := func(selection, text string) *wlproto.ZwlrDataControlSourceV1 {
		s := manager.CreateDataSource()
		s.OnSend = func(mime string, fd int) {
			o.mu.Lock()
			o.requests = append(o.requests, selection+" "+mime)
			o.mu.Unlock()
			f := os.NewFile(uintptr(fd), selection)
			_, _ = f.WriteString(text)
			_ = f.Close()
		}
		for _, mime := range Offered {
			s.Offer(mime)
		}
		return s
	}
	if clipboard != "" {
		device.SetSelection(source("clipboard", clipboard))
	}
	if primary != "" {
		device.SetPrimarySelection(source("primary", primary))
	}
	started <- d.Roundtrip()
	for {
		select {
		case <-o.stop:
			return // Close destroys the sources and the device with it
		default:
		}
		if d.Dispatch(20*time.Millisecond) != nil {
			return
		}
	}
}

// Requests is what clients asked for so far: `<selection> <MIME type>`
// each.
func (o *SelectionOwner) Requests() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.requests)
}

// Stop gives the selections up and waits for the owner to end.
func (o *SelectionOwner) Stop() {
	o.once.Do(func() { close(o.stop) })
	<-o.done
}
