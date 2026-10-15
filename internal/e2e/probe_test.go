package e2e

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// probe is a test client of the test compositor, owned by the test's
// goroutine: a small opaque layer surface that records what its pointer
// sees, and a data-control device that reads the selections. It checks the
// injectors and the selection owner without logis.
type probe struct {
	t       *testing.T
	d       *wlproto.Display
	globals map[string]global
	reg     *wlproto.WlRegistry

	configured bool
	pointer    []string   // "enter x y", "motion x y", "button b s", "axis …"
	at         [2]float64 // the last surface point the pointer was at
	mimes      map[uint32][]string
	selection  *wlproto.ZwlrDataControlOfferV1
	primary    *wlproto.ZwlrDataControlOfferV1
}

func newProbe(t *testing.T, c *Compositor) *probe {
	t.Helper()
	d, err := ConnectVerified(c.Socket(), c.PID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	p := &probe{t: t, d: d, globals: map[string]global{}, mimes: map[uint32][]string{}}
	p.reg = d.Registry()
	p.reg.OnGlobal = func(name uint32, iface string, version uint32) { p.globals[iface] = global{name, version} }
	p.roundtrip()
	return p
}

func (p *probe) bind(iface *wlproto.Interface, most uint32) wlproto.Object {
	g, ok := p.globals[iface.Name]
	if !ok {
		p.t.Fatalf("no %s", iface.Name)
	}
	return p.reg.Bind(g.name, iface, min(g.version, most))
}

func (p *probe) roundtrip() {
	p.t.Helper()
	if err := p.d.Roundtrip(); err != nil {
		p.t.Fatal(err)
	}
}

func (p *probe) until(what string, done func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(Patience)
	for !done() {
		if time.Now().After(deadline) {
			p.t.Fatalf("probe: no %s in %v (pointer saw %q)", what, Patience, p.pointer)
		}
		if err := p.d.Dispatch(50 * time.Millisecond); err != nil {
			p.t.Fatal(err)
		}
	}
}

// surface shows a w×h opaque grey layer surface on the top layer, centred,
// and listens to the seat's pointer.
func (p *probe) surface(dir string, w, h int32) {
	p.t.Helper()
	compositor, _ := p.bind(wlproto.WlCompositorInterface, 4).(*wlproto.WlCompositor)
	shell, _ := p.bind(wlproto.ZwlrLayerShellV1Interface, 4).(*wlproto.ZwlrLayerShellV1)
	shm, _ := p.bind(wlproto.WlShmInterface, 1).(*wlproto.WlShm)
	seat, _ := p.bind(wlproto.WlSeatInterface, 5).(*wlproto.WlSeat)
	pixels := make([]byte, w*h*4)
	for i := range pixels {
		pixels[i] = 0x80
	}
	path := filepath.Join(dir, "probe.shm")
	if err := os.WriteFile(path, pixels, 0o600); err != nil {
		p.t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		p.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	pool := shm.CreatePool(int(f.Fd()), int32(len(pixels)))
	buffer := pool.CreateBuffer(0, w, h, w*4, wlproto.WlShmFormatXrgb8888)
	surface := compositor.CreateSurface()
	layer := shell.GetLayerSurface(surface, nil, wlproto.ZwlrLayerShellV1LayerTop, "logis-test-probe")
	layer.SetSize(uint32(w), uint32(h))
	layer.OnConfigure = func(serial, _, _ uint32) {
		layer.AckConfigure(serial)
		surface.Attach(buffer, 0, 0)
		surface.Damage(0, 0, w, h)
		surface.Commit()
		p.configured = true
	}
	surface.Commit()
	pointer := seat.GetPointer()
	pointer.OnEnter = func(_ uint32, _ *wlproto.WlSurface, x, y wlproto.Fixed) {
		p.at = [2]float64{x.Float(), y.Float()}
		p.pointer = append(p.pointer, "enter")
	}
	pointer.OnMotion = func(_ uint32, x, y wlproto.Fixed) {
		p.at = [2]float64{x.Float(), y.Float()}
		p.pointer = append(p.pointer, "motion")
	}
	pointer.OnButton = func(_, _, button, state uint32) {
		p.pointer = append(p.pointer, "button "+fmt.Sprint(button)+" "+fmt.Sprint(state))
	}
	pointer.OnAxisDiscrete = func(_ uint32, discrete int32) {
		p.pointer = append(p.pointer, "discrete "+fmt.Sprint(discrete))
	}
	pointer.OnAxisValue120 = func(_ uint32, v int32) {
		p.pointer = append(p.pointer, "discrete "+fmt.Sprint(v/120))
	}
	pointer.OnAxis = func(_, _ uint32, v wlproto.Fixed) {
		p.pointer = append(p.pointer, "axis "+fmt.Sprint(v.Float()))
	}
	p.until("the probe's configure", func() bool { return p.configured })
	// The configure handler's attach and commit are only queued: send them
	// and wait until the compositor has mapped the surface, so a pointer
	// moved next lands on it. A compositor need not refocus a pointer that
	// stands still when a surface maps under it (sway 1.12 does not).
	p.roundtrip()
}

// selections listens to the selections through data control.
func (p *probe) selections() {
	p.t.Helper()
	seat, _ := p.bind(wlproto.WlSeatInterface, 1).(*wlproto.WlSeat)
	manager, _ := p.bind(wlproto.ZwlrDataControlManagerV1Interface, 2).(*wlproto.ZwlrDataControlManagerV1)
	device := manager.GetDataDevice(seat)
	device.OnDataOffer = func(offer *wlproto.ZwlrDataControlOfferV1) {
		id := offer.ID()
		offer.OnOffer = func(mime string) { p.mimes[id] = append(p.mimes[id], mime) }
	}
	device.OnSelection = func(offer *wlproto.ZwlrDataControlOfferV1) { p.selection = offer }
	device.OnPrimarySelection = func(offer *wlproto.ZwlrDataControlOfferV1) { p.primary = offer }
	p.roundtrip()
}

// receive reads offer's text as mime.
func (p *probe) receive(offer *wlproto.ZwlrDataControlOfferV1, mime string) string {
	p.t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		p.t.Fatal(err)
	}
	offer.Receive(mime, int(w.Fd()))
	p.roundtrip()
	_ = w.Close()
	_ = r.SetReadDeadline(time.Now().Add(Patience))
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		p.t.Fatal(err)
	}
	return string(data)
}
