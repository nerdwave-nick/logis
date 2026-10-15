package window

import (
	"slices"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// How much room a panel has: the list fits its rows into the logical
// height of the output it shows on (scene.FitIn). The compositor's outputs
// are described by xdg-output (optional: without it the room is unknown,
// and panels ask for their full height); the compositor says which output
// a panel landed on with wl_surface.enter. Before any panel has shown, the
// room is the shortest output's, so a first panel never reaches past an
// edge; the output it enters then re-fits it.

// output is one of the compositor's outputs.
type output struct {
	name uint32 // its registry name
	wl   *wlproto.WlOutput
	xdg  *wlproto.ZxdgOutputV1
	// height is its logical height, 0 until xdg-output says it.
	height uint32
}

// outputs are the outputs described so far, the one the last panel
// entered, and who is told of the room a panel's output gives.
type outputs struct {
	manager *wlproto.ZxdgOutputManagerV1
	all     []*output
	last    *output
	sink    func(room uint32)
}

// bindOutputs binds xdg-output and every output advertised now; hotplugged
// ones come through added.
func (w *Window) bindOutputs() {
	w.outputs.manager, _ = w.Bind(wlproto.ZxdgOutputManagerV1Interface, 3).(*wlproto.ZxdgOutputManagerV1)
	for _, g := range w.conn.globals {
		w.outputAdded(g)
	}
}

// outputAdded describes global g if it is an output.
func (w *Window) outputAdded(g Global) {
	o := &w.outputs
	if o.manager == nil || g.Interface != wlproto.WlOutputInterface.Name {
		return
	}
	wl, _ := w.conn.registry.Bind(g.Name, wlproto.WlOutputInterface, min(g.Version, 4)).(*wlproto.WlOutput)
	out := &output{name: g.Name, wl: wl, xdg: o.manager.GetXdgOutput(wl)}
	out.xdg.OnLogicalSize = func(_, height int32) {
		out.height = uint32(max(height, 0))
		if out == o.last && w.panel != nil {
			o.tell(out)
		}
	}
	o.all = append(o.all, out)
}

// outputRemoved forgets global g if it is a described output.
func (w *Window) outputRemoved(g Global) {
	o := &w.outputs
	i := slices.IndexFunc(o.all, func(out *output) bool { return out.name == g.Name })
	if i < 0 {
		return
	}
	o.all[i].release()
	if o.last == o.all[i] {
		o.last = nil
	}
	o.all = slices.Delete(o.all, i, i+1)
}

// entered: the compositor put panel p on wl.
func (w *Window) entered(p *panel, wl *wlproto.WlOutput) {
	o := &w.outputs
	i := slices.IndexFunc(o.all, func(out *output) bool { return out.wl == wl })
	if i < 0 || p != w.panel {
		return
	}
	o.last = o.all[i]
	o.tell(o.last)
}

// tell tells the sink out's height, once it is known.
func (o *outputs) tell(out *output) {
	if out.height > 0 && o.sink != nil {
		o.sink(out.height)
	}
}

// Room is the logical height of the output the next panel shows on, as
// far as it is known: the one the last panel entered, else the shortest
// output described; 0 when none is known (no xdg-output).
func (w *Window) Room() uint32 {
	o := &w.outputs
	if o.last != nil && o.last.height > 0 {
		return o.last.height
	}
	var room uint32
	for _, out := range o.all {
		if out.height > 0 && (room == 0 || out.height < room) {
			room = out.height
		}
	}
	return room
}

// SetRoomSink tells sink, on the window's goroutine, the logical height of
// the output the visible panel shows on: when the compositor puts it on
// one, and when that output's size changes under it.
func (w *Window) SetRoomSink(sink func(room uint32)) { w.outputs.sink = sink }

func (out *output) release() {
	out.xdg.Destroy()
	if out.wl.Version() >= 3 {
		out.wl.Release()
	} else {
		out.wl.Destroy()
	}
}

// releaseOutputs releases every output and xdg-output.
func (w *Window) releaseOutputs() {
	o := &w.outputs
	for _, out := range o.all {
		out.release()
	}
	o.all, o.last = nil, nil
	if o.manager != nil {
		o.manager.Destroy()
		o.manager = nil
	}
}
