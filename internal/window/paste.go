package window

import (
	"golang.org/x/sys/unix"

	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// clipboard is paste from the clipboard (wl_data_device) and the primary
// selection (zwp_primary_selection_device_v1), both used when offered
// (Rust src/window/paste.rs). The devices track the selection offers and
// their MIME types as the compositor announces them to the focused window.
//
// A paste picks the best text type on offer (input.ChooseMIME), asks the
// owner to write it into a pipe, and reads that pipe without blocking: the
// read end is non-blocking and watched in the display's poll, so the frame
// loop keeps running while a slow owner writes. Once the owner closes the
// pipe (or input.PasteLimit is reached), the text goes to the sink as an
// Insert edit at the query's cursor. A newer paste replaces one still
// being read; hiding the panel drops it. Without a protocol, its paste does
// nothing (trace `paste <selection> unavailable`).
//
// Trace events: `paste <selection> <MIME type>` when a read starts,
// `paste <selection> none` when nothing text-like is on offer, and
// `pasted <selection> <bytes>` when it ends.
type clipboard struct {
	display        *wlproto.Display
	dataManager    *wlproto.WlDataDeviceManager
	primaryManager *wlproto.ZwpPrimarySelectionDeviceManagerV1
	data           *wlproto.WlDataDevice
	primary        *wlproto.ZwpPrimarySelectionDeviceV1
	// The offers alive and their MIME types; the current selections.
	dataOffers       map[*wlproto.WlDataOffer][]string
	dataSelection    *wlproto.WlDataOffer
	primaryOffers    map[*wlproto.ZwpPrimarySelectionOfferV1][]string
	primarySelection *wlproto.ZwpPrimarySelectionOfferV1
	reading          *reading
	// sink receives the pasted text (nil: it is dropped).
	sink func(input.Action)
}

// reading is a paste in progress: the pipe's read end and what came so far.
type reading struct {
	selection input.Selection
	fd        int
	bytes     input.PasteBytes
}

// bindClipboard binds the selection managers the compositor offers and
// gets their devices for seat (none without a seat).
func bindClipboard(w *Window, seat *wlproto.WlSeat) *clipboard {
	c := &clipboard{
		display:       w.display,
		dataOffers:    map[*wlproto.WlDataOffer][]string{},
		primaryOffers: map[*wlproto.ZwpPrimarySelectionOfferV1][]string{},
	}
	c.dataManager, _ = w.Bind(wlproto.WlDataDeviceManagerInterface, 3).(*wlproto.WlDataDeviceManager)
	c.primaryManager, _ = w.Bind(wlproto.ZwpPrimarySelectionDeviceManagerV1Interface, 1).(*wlproto.ZwpPrimarySelectionDeviceManagerV1)
	if seat == nil {
		return c
	}
	if c.dataManager != nil {
		c.data = c.dataManager.GetDataDevice(seat)
		c.listenData(c.data)
	}
	if c.primaryManager != nil {
		c.primary = c.primaryManager.GetDevice(seat)
		c.listenPrimary(c.primary)
	}
	return c
}

func (c *clipboard) listenData(d *wlproto.WlDataDevice) {
	d.OnDataOffer = func(o *wlproto.WlDataOffer) {
		c.dataOffers[o] = nil
		o.OnOffer = func(mime string) { c.dataOffers[o] = append(c.dataOffers[o], mime) }
	}
	d.OnSelection = func(o *wlproto.WlDataOffer) {
		if old := c.dataSelection; old != nil && old != o {
			c.dropData(old)
		}
		c.dataSelection = o
	}
	// logis accepts no drag and drop: a dragged offer is declined at once.
	d.OnEnter = func(_ uint32, _ *wlproto.WlSurface, _, _ wlproto.Fixed, o *wlproto.WlDataOffer) {
		if o != nil && o != c.dataSelection {
			c.dropData(o)
		}
	}
}

func (c *clipboard) listenPrimary(d *wlproto.ZwpPrimarySelectionDeviceV1) {
	d.OnDataOffer = func(o *wlproto.ZwpPrimarySelectionOfferV1) {
		c.primaryOffers[o] = nil
		o.OnOffer = func(mime string) { c.primaryOffers[o] = append(c.primaryOffers[o], mime) }
	}
	d.OnSelection = func(o *wlproto.ZwpPrimarySelectionOfferV1) {
		if old := c.primarySelection; old != nil && old != o {
			c.dropPrimary(old)
		}
		c.primarySelection = o
	}
}

func (c *clipboard) dropData(o *wlproto.WlDataOffer) {
	if _, live := c.dataOffers[o]; live {
		delete(c.dataOffers, o)
		o.Destroy()
	}
}

func (c *clipboard) dropPrimary(o *wlproto.ZwpPrimarySelectionOfferV1) {
	if _, live := c.primaryOffers[o]; live {
		delete(c.primaryOffers, o)
		o.Destroy()
	}
}

// start starts reading sel's text, replacing a paste in progress.
func (c *clipboard) start(sel input.Selection) {
	c.cancel()
	name := sel.Name()
	var (
		device  bool
		mimes   []string
		receive func(mime string, fd int)
	)
	switch sel {
	case input.Clipboard:
		device = c.data != nil
		if o := c.dataSelection; o != nil {
			mimes, receive = c.dataOffers[o], o.Receive
		}
	case input.Primary:
		device = c.primary != nil
		if o := c.primarySelection; o != nil {
			mimes, receive = c.primaryOffers[o], o.Receive
		}
	}
	mime, text := input.ChooseMIME(mimes)
	switch {
	case !device:
		trace.Emit("paste %s unavailable", name)
		return
	case receive == nil || !text:
		trace.Emit("paste %s none", name)
		return
	}
	trace.Emit("paste %s %s", name, mime)
	var p [2]int
	// A blocking read would stall the frame loop on a slow owner.
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return
	}
	receive(mime, p[1]) // libwayland sends a duplicate
	_ = unix.Close(p[1])
	c.reading = &reading{selection: sel, fd: p[0]}
	c.display.Watch(p[0], c.pump)
}

// cancel drops a paste in progress (the panel was hidden or replaced).
func (c *clipboard) cancel() {
	if r := c.reading; r != nil {
		c.reading = nil
		c.display.Unwatch(r.fd)
		_ = unix.Close(r.fd)
	}
}

// pump reads what the owner has written so far; once it closed the pipe
// (or the limit is reached), the text goes to the sink.
func (c *clipboard) pump() {
	r := c.reading
	if r == nil {
		return
	}
	var chunk [8192]byte
	for {
		n, err := unix.Read(r.fd, chunk[:])
		if err == unix.EINTR || n > 0 && !r.bytes.Push(chunk[:n]) {
			continue
		}
		if err == unix.EAGAIN {
			return
		}
		// The end, the limit, or a broken pipe: the paste ends with what
		// came.
		break
	}
	c.cancel()
	text := r.bytes.Text()
	trace.Emit("pasted %s %d", r.selection.Name(), len(text))
	if text != "" && c.sink != nil {
		c.sink(input.Insert(text))
	}
}

// releaseDevices drops the paste, the offers and both devices (the seat
// went away).
func (c *clipboard) releaseDevices() {
	c.cancel()
	for o := range c.dataOffers {
		c.dropData(o)
	}
	for o := range c.primaryOffers {
		c.dropPrimary(o)
	}
	c.dataSelection, c.primarySelection = nil, nil
	if c.data != nil {
		if c.data.Version() >= 2 {
			c.data.Release()
		} else {
			c.data.Destroy()
		}
		c.data = nil
	}
	if c.primary != nil {
		c.primary.Destroy()
		c.primary = nil
	}
}

// close releases everything, the managers last.
func (c *clipboard) close() {
	c.releaseDevices()
	if c.dataManager != nil {
		c.dataManager.Destroy() // no destructor before version 4
		c.dataManager = nil
	}
	if c.primaryManager != nil {
		c.primaryManager.Destroy()
		c.primaryManager = nil
	}
}
