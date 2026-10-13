package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/window"
)

// debugSize is the surface the debug panel requests: Rust's 640×360 body
// plus 24 logical pixels of glow margin on every side (DEBUG_SIZE).
var debugSize = scale.Size{W: 688, H: 408}

// Forge crimson & brass, premultiplied: the brass rim and the soot body of
// Rust's debug panel. A cleared frame stands in for its shapes and text
// until the renderer exists.
var (
	brass = [4]float32{0.83, 0.63, 0.09, 1}
	soot  = [4]float32{0.085 * 0.86, 0.055 * 0.86, 0.045 * 0.86, 0.86}
)

// keyEscape is evdev's KEY_ESC, the same key on every layout.
const keyEscape = 1

// isDebugPanel recognises `logis daemon --debug-panel`.
func isDebugPanel(args []string) bool {
	return len(args) == 2 && args[0] == "daemon" && args[1] == "--debug-panel"
}

// debugPanel is `logis daemon --debug-panel`: it connects, wakes the GPU,
// shows the panel and keeps it, redrawing only on configure and scale
// changes, until Escape dismisses it, the compositor closes it, or SIGINT
// or SIGTERM ends it (exit 0). It is not the session daemon: it neither
// listens nor serves sessions. Losing the compositor is exit 2.
func debugPanel(stderr io.Writer) int {
	w, err := window.Open()
	if err != nil {
		return fail(stderr, err.Error())
	}
	defer w.Close()
	info := w.GPU()
	_, _ = fmt.Fprintf(stderr, "logis: the machine spirit wakes on %s (%s)\n", info.Renderer, info.Version)
	w.SetLog(stderr)
	stop := &escape{}
	w.SetKeyboard(stop)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		if _, ok := <-signals; ok {
			stop.signalled.Store(true)
			w.Wake()
		}
	}()
	if err := w.Show(debugSize, paintDebug); err != nil {
		return fail(stderr, err.Error())
	}
	for w.Visible() && !stop.done() {
		if err := w.Dispatch(-1); err != nil {
			return fail(stderr, err.Error())
		}
	}
	return 0
}

// paintDebug clears the frame brass, then everything but a rim of two
// logical pixels soot: the rim shows the scale is honoured to the pixel.
func paintDebug(g window.Geometry) bool {
	rim := int(g.Scale.Physical(scale.Size{W: 2, H: 2}).W)
	width, height := int(g.Physical.W), int(g.Physical.H)
	gles.NoScissor()
	gles.ClearColor(brass[0], brass[1], brass[2], brass[3])
	gles.Clear()
	if width > 2*rim && height > 2*rim {
		gles.Scissor(rim, rim, width-2*rim, height-2*rim)
		gles.ClearColor(soot[0], soot[1], soot[2], soot[3])
		gles.Clear()
		gles.NoScissor()
	}
	return false
}
