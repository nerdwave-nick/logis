package wlproto

import (
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

var niriLike = []wltest.Global{
	{Interface: "wl_compositor", Version: 6},
	{Interface: "wl_seat", Version: 9},
	{Interface: "zwlr_layer_shell_v1", Version: 4},
	{Interface: "wl_data_device_manager", Version: 3},
}

// connect starts a fake compositor and connects a display to it.
func connect(t *testing.T, globals []wltest.Global) (*Display, *wltest.Compositor) {
	t.Helper()
	comp, err := wltest.Serve(t.TempDir(), "wayland-test", globals)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(comp.Close)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: comp.Path}); err != nil {
		t.Fatal(err)
	}
	d, err := ConnectFd(fd)
	if err != nil {
		t.Fatal(err)
	}
	return d, comp
}

type global struct {
	name    uint32
	iface   string
	version uint32
}

// globals reads the registry in one roundtrip.
func globals(t *testing.T, d *Display) (*WlRegistry, []global) {
	t.Helper()
	r := d.Registry()
	var got []global
	r.OnGlobal = func(name uint32, iface string, version uint32) {
		got = append(got, global{name, iface, version})
	}
	if err := d.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	return r, got
}

func bind[T Object](t *testing.T, r *WlRegistry, gs []global, iface *Interface, version uint32) T {
	t.Helper()
	for _, g := range gs {
		if g.iface == iface.Name {
			obj, ok := r.Bind(g.name, iface, version).(T)
			if !ok {
				t.Fatalf("binding %s gave %T", iface.Name, obj)
			}
			return obj
		}
	}
	t.Fatalf("%s is not advertised", iface.Name)
	panic("unreachable")
}

func TestTheRegistryAnnouncesTheGlobalsInOrder(t *testing.T) {
	d, _ := connect(t, niriLike)
	defer d.Close()
	_, got := globals(t, d)
	want := []global{
		{1, "wl_compositor", 6},
		{2, "wl_seat", 9},
		{3, "zwlr_layer_shell_v1", 4},
		{4, "wl_data_device_manager", 3},
	}
	if !slices.Equal(got, want) {
		t.Errorf("globals %v, want %v", got, want)
	}
}

func TestRequestsReachTheCompositorWithTheirArguments(t *testing.T) {
	d, comp := connect(t, niriLike)
	defer d.Close()
	type seen struct {
		object uint32
		opcode uint16
		first  uint32
	}
	requests := make(chan seen, 16)
	comp.OnRequest(func(_ *wltest.Client, r wltest.Request) {
		requests <- seen{r.Object, r.Opcode, wltest.NewReader(r).Uint()}
	})
	r, gs := globals(t, d)
	c := bind[*WlCompositor](t, r, gs, WlCompositorInterface, 4)
	s := c.CreateSurface()
	s.SetBufferScale(2)
	if err := d.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	if c.Version() != 4 || s.Version() != 4 {
		t.Errorf("versions %d and %d, want the bound 4", c.Version(), s.Version())
	}
	want := []seen{
		{r.ID(), 0, 1},         // wl_registry.bind of global 1
		{c.ID(), 0, s.ID()},    // wl_compositor.create_surface with the new id
		{s.ID(), 8, uint32(2)}, // wl_surface.set_buffer_scale 2
	}
	for _, w := range want {
		select {
		case got := <-requests:
			if got != w {
				t.Errorf("request %+v, want %+v", got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("request %+v never arrived", w)
		}
	}
}

// keyboardSession has the fake compositor give the client a keyboard and
// then send it every wl_keyboard event once.
func keyboardSession(t *testing.T, comp *wltest.Compositor, keymap string) {
	t.Helper()
	var surface uint32
	comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		rd := wltest.NewReader(r)
		switch {
		case r.Object == cl.Bound("wl_compositor") && r.Opcode == 0:
			surface = rd.Uint()
		case r.Object == cl.Bound("wl_seat") && r.Opcode == 1: // get_keyboard
			k := rd.Uint()
			mem, err := os.CreateTemp(t.TempDir(), "keymap")
			if err != nil {
				t.Error(err)
				return
			}
			_ = os.Remove(mem.Name())
			_, _ = mem.WriteString(keymap)
			keys := binary.NativeEndian.AppendUint32(binary.NativeEndian.AppendUint32(nil, 30), 31)
			_ = cl.Send(k, 0, uint32(1), wltest.Fd(mem.Fd()), uint32(len(keymap)))
			_ = mem.Close()
			if surface == 0 {
				return // enter and leave need a surface
			}
			_ = cl.Send(k, 1, uint32(7), surface, keys)
			_ = cl.Send(k, 3, uint32(8), uint32(1234), uint32(30), uint32(1))
			_ = cl.Send(k, 4, uint32(9), uint32(1), uint32(0), uint32(2), uint32(0))
			_ = cl.Send(k, 5, int32(25), int32(600))
			_ = cl.Send(k, 2, uint32(10), surface)
		}
		for _, fd := range r.Fds {
			_ = syscall.Close(fd)
		}
	})
}

func TestEventsReachTheirHandlersWithTheirArguments(t *testing.T) {
	d, comp := connect(t, niriLike)
	defer d.Close()
	keyboardSession(t, comp, "xkb_keymap {};")
	r, gs := globals(t, d)
	c := bind[*WlCompositor](t, r, gs, WlCompositorInterface, 6)
	surface := c.CreateSurface()
	seat := bind[*WlSeat](t, r, gs, WlSeatInterface, 9)
	var log []string
	var keymap string
	seat.OnCapabilities = func(caps uint32) {
		if caps&WlSeatCapabilityKeyboard == 0 {
			return
		}
		k := seat.GetKeyboard()
		k.OnKeymap = func(format uint32, fd int, size uint32) {
			f := os.NewFile(uintptr(fd), "keymap")
			defer func() { _ = f.Close() }()
			buf := make([]byte, size)
			_, _ = f.ReadAt(buf, 0)
			keymap = string(buf)
			log = append(log, "keymap")
		}
		k.OnEnter = func(serial uint32, s *WlSurface, keys []byte) {
			if s != surface {
				t.Errorf("enter names %v, want the created surface", s)
			}
			pressed := []uint32{binary.NativeEndian.Uint32(keys), binary.NativeEndian.Uint32(keys[4:])}
			if serial != 7 || !slices.Equal(pressed, []uint32{30, 31}) {
				t.Errorf("enter serial %d keys %v", serial, pressed)
			}
			log = append(log, "enter")
		}
		k.OnKey = func(serial, time, key, state uint32) {
			if serial != 8 || time != 1234 || key != 30 || state != WlKeyboardKeyStatePressed {
				t.Errorf("key %d %d %d %d", serial, time, key, state)
			}
			log = append(log, "key")
		}
		k.OnModifiers = func(serial, depressed, latched, locked, group uint32) {
			if depressed != 1 || locked != 2 {
				t.Errorf("modifiers %d %d %d %d", depressed, latched, locked, group)
			}
			log = append(log, "modifiers")
		}
		k.OnRepeatInfo = func(rate, delay int32) {
			if rate != 25 || delay != 600 {
				t.Errorf("repeat %d %d", rate, delay)
			}
			log = append(log, "repeat")
		}
		k.OnLeave = func(serial uint32, s *WlSurface) {
			if s != surface {
				t.Errorf("leave names %v", s)
			}
			log = append(log, "leave")
		}
	}
	if err := d.Roundtrip(); err != nil { // the binds reach the compositor
		t.Fatal(err)
	}
	for _, cl := range comp.Clients() {
		_ = cl.Send(cl.Bound("wl_seat"), 0, uint32(WlSeatCapabilityKeyboard|WlSeatCapabilityPointer))
	}
	for range 3 {
		if err := d.Roundtrip(); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"keymap", "enter", "key", "modifiers", "repeat", "leave"}; !slices.Equal(log, want) {
		t.Errorf("events %v, want %v", log, want)
	}
	if keymap != "xkb_keymap {};" {
		t.Errorf("keymap %q", keymap)
	}
}

func openFds(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("no /proc/self/fd")
	}
	return len(entries)
}

func TestFdsOfUnhandledEventsAreClosed(t *testing.T) {
	d, comp := connect(t, niriLike)
	defer d.Close()
	keyboardSession(t, comp, "xkb_keymap {};")
	r, gs := globals(t, d)
	seat := bind[*WlSeat](t, r, gs, WlSeatInterface, 9)
	// Ten keyboards, none with a keymap handler.
	for range 10 {
		seat.GetKeyboard()
	}
	if err := d.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	before := openFds(t)
	for range 10 {
		seat.GetKeyboard()
	}
	for range 2 {
		if err := d.Roundtrip(); err != nil {
			t.Fatal(err)
		}
	}
	if after := openFds(t); after > before {
		t.Errorf("%d fds open after ten unhandled keymaps, %d before", after, before)
	}
}

func TestAnObjectTheCompositorCreatesReceivesItsOwnEvents(t *testing.T) {
	d, comp := connect(t, niriLike)
	defer d.Close()
	const offerID = 0xff000000 // the first id a compositor allocates
	comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		if r.Object == cl.Bound("wl_data_device_manager") && r.Opcode == 1 { // get_data_device
			device := wltest.NewReader(r).Uint()
			_ = cl.Send(device, 0, uint32(offerID))             // data_offer (new id)
			_ = cl.Send(offerID, 0, "text/plain;charset=utf-8") // wl_data_offer.offer
			_ = cl.Send(device, 5, uint32(offerID))             // selection
		}
	})
	r, gs := globals(t, d)
	seat := bind[*WlSeat](t, r, gs, WlSeatInterface, 9)
	manager := bind[*WlDataDeviceManager](t, r, gs, WlDataDeviceManagerInterface, 3)
	device := manager.GetDataDevice(seat)
	var offer *WlDataOffer
	var mimes []string
	device.OnDataOffer = func(o *WlDataOffer) {
		offer = o
		o.OnOffer = func(mime string) { mimes = append(mimes, mime) }
	}
	var selection *WlDataOffer
	device.OnSelection = func(o *WlDataOffer) { selection = o }
	for range 2 {
		if err := d.Roundtrip(); err != nil {
			t.Fatal(err)
		}
	}
	if offer == nil || offer.ID() != offerID {
		t.Fatalf("offer %v, want id %#x", offer, offerID)
	}
	if !slices.Equal(mimes, []string{"text/plain;charset=utf-8"}) {
		t.Errorf("mime types %q", mimes)
	}
	if selection != offer {
		t.Errorf("selection %v, want the offer", selection)
	}
}

func TestDestroyedProxiesReleaseTheirHandles(t *testing.T) {
	d, _ := connect(t, niriLike)
	r, gs := globals(t, d)
	c := bind[*WlCompositor](t, r, gs, WlCompositorInterface, 6)
	base := d.Live() // the registry and the compositor
	if base != 2 {
		t.Errorf("%d live proxies, want the registry and the compositor", base)
	}
	s := c.CreateSurface()
	region := c.CreateRegion()
	done := false
	cb := d.Sync()
	cb.OnDone = func(uint32) { done = true }
	if got := d.Live(); got != base+3 {
		t.Errorf("%d live, want %d", got, base+3)
	}
	if err := d.Roundtrip(); err != nil {
		t.Fatal(err)
	}
	if !done || cb.Valid() {
		t.Errorf("callback done %v, still valid %v: a destructor event ends it", done, cb.Valid())
	}
	s.Destroy()
	region.Destroy()
	region.Destroy() // twice is harmless
	if got := d.Live(); got != base {
		t.Errorf("%d live after destroying, want %d", got, base)
	}
	objects.Lock()
	total := len(objects.byHandle)
	objects.Unlock()
	d.Close()
	objects.Lock()
	after := len(objects.byHandle)
	objects.Unlock()
	if total-after != base {
		t.Errorf("close released %d handles, want %d", total-after, base)
	}
	if c.Valid() || r.Valid() {
		t.Error("proxies are still valid after Close")
	}
}

func TestAHangUpEndsDispatchWithAnError(t *testing.T) {
	d, comp := connect(t, niriLike)
	defer d.Close()
	globals(t, d)
	comp.Close()
	result := make(chan error, 1)
	go func() {
		var err error
		for err == nil {
			err = d.Dispatch(-1)
		}
		result <- err
	}()
	select {
	case err := <-result:
		var wlErr *Error
		if !errors.As(err, &wlErr) {
			t.Errorf("error %v is not a connection error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not notice the hang-up")
	}
}

func TestWakeAndTimeoutEndAWaitingDispatch(t *testing.T) {
	d, _ := connect(t, niriLike)
	defer d.Close()
	globals(t, d)
	start := time.Now()
	if err := d.Dispatch(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < 30*time.Millisecond {
		t.Errorf("returned after %v, before the 30 ms timeout", waited)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		d.Wake()
	}()
	result := make(chan error, 1)
	go func() { result <- d.Dispatch(-1) }()
	select {
	case err := <-result:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		d.Wake()
		t.Fatal("Wake did not end the wait")
	}
}

func TestFixedPointRoundTrips(t *testing.T) {
	for _, v := range []float64{0, 1, -1, 0.5, 12.25, -3.75, 1.3} {
		f := FixedFrom(v)
		if got := f.Float(); got-v > 1.0/512 || v-got > 1.0/512 {
			t.Errorf("FixedFrom(%v).Float() = %v", v, got)
		}
	}
	if FixedFrom(1) != 256 || FixedFrom(-0.5) != -128 {
		t.Errorf("FixedFrom(1) = %d, FixedFrom(-0.5) = %d", FixedFrom(1), FixedFrom(-0.5))
	}
}
