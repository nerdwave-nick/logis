package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/window"
)

// debugPanel is `logis daemon --debug-panel [--config PATH]`: it reads the
// config once (for focus-loss), connects, wakes the GPU, shows the panel
// and keeps it, redrawing only on configure, scale, selection and query
// changes, until it is dismissed (Escape, Ctrl+G, Ctrl+[, or focus loss
// unless the config keeps it), accepted (Return: the selected row is
// printed), closed by the compositor, or SIGINT or SIGTERM ends it (exit
// 0). It is not the session daemon: it neither listens nor serves
// sessions. Losing the compositor is exit 2.
func debugPanel(src *config.Source, stdout, stderr io.Writer) int {
	if src == nil {
		s := config.NewSource("")
		src = &s
	}
	cfg, err := src.Load()
	if err != nil {
		return fail(stderr, config.Heresy(err))
	}
	// Fonts are resolved at start on a goroutine of their own (Rust
	// Windows::connect): fontconfig's start-up overlaps the compositor's
	// and the GPU's, off the first frame's critical path.
	fonts := text.ResolveSystem()
	w, err := window.Open()
	if err != nil {
		discard(fonts)
		return fail(stderr, err.Error())
	}
	defer w.Close()
	info := w.GPU()
	_, _ = fmt.Fprintf(stderr, "logis: the machine spirit wakes on %s (%s)\n", info.Renderer, info.Version)
	// The renderer is made at start on the window's GL context (shaders
	// compiled now, not on the first frame) and goes before the context.
	r, err := render.New()
	if err != nil {
		discard(fonts)
		return fail(stderr, err.Error())
	}
	defer r.Close()
	// Without fonts the cogitator still opens, only without text.
	if f, err := fonts.Wait(); err != nil {
		_, _ = fmt.Fprintf(stderr, "logis: %v; the cogitator stays mute\n", err)
	} else {
		r.SetFonts(f) // the renderer owns them now
	}
	w.SetLog(stderr)
	// The Compose table is loaded now, at start, not on the first key.
	session := &debugSession{stdout: stdout, redraw: w.RequestRedraw}
	w.SetKeyboard(debugKeyboard(cfg, session, stderr))
	var signalled atomic.Bool
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		if _, ok := <-signals; ok {
			signalled.Store(true)
			w.Wake()
		}
	}()
	trace.Emit("show debug-panel")
	if err := w.Show(debugSize, paintDebug(r, session)); err != nil {
		return fail(stderr, err.Error())
	}
	for w.Visible() && !session.done && !signalled.Load() {
		if err := w.Dispatch(-1); err != nil {
			return fail(stderr, err.Error())
		}
	}
	return 0
}

// paintDebug draws the debug scene for each frame's geometry, with the
// session's selected row and query, into the window surface through r.
// The scene is still: it never animates.
func paintDebug(r *render.Renderer, s *debugSession) window.Painter {
	return func(g window.Geometry) bool {
		r.Draw(render.Target{Size: g.Physical, Scale: g.Scale}, debugSceneWithQuery(g.Logical, s.row, &s.query))
		return false
	}
}

// discard waits for fonts nobody will draw with and releases them.
func discard(p *text.PendingFonts) {
	if f, err := p.Wait(); err == nil {
		f.Close()
	}
}
