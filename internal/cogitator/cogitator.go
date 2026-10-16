// Package cogitator is the window-backed resolver of the daemon (Rust
// src/daemon/window): sessions are shown in the cogitator window.
//
// The window lives on a goroutine of its own, locked to its thread, which
// owns the daemon's single compositor connection, the GPU (woken at start,
// before the daemon reports that it listens), the renderer, the fonts and
// the keyboard. The daemon's goroutine talks to it through Resolver: each
// call becomes a command queued for the window's goroutine plus a Wake of
// its poll, so neither busy-waits and an idle window sleeps in poll.
// Answers go back through the daemon's Host; a lost compositor ends the
// daemon with exit 2.
package cogitator

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/icons"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/window"
)

// Resolver is the resolver the daemon holds; the window's goroutine does
// the work. It is also a daemon.Configurer, Toaster, Rethemer and
// Activator.
type Resolver struct {
	mu    sync.Mutex
	queue []func(*ui)
	wake  func()
}

// Start opens the window on a goroutine of its own: it connects to the
// compositor (refusing one that cannot host the cogitator), wakes the GPU,
// compiles the renderer's shaders and resolves the default theme's fonts.
// LOGIS_MOTION may turn motion off or slow it (motion.FromEnv); it
// outranks the theme's timings. It returns once that is done, or why it
// failed. Diagnostics go to log.
func Start(host daemon.Host, log io.Writer) (*Resolver, error) {
	envMotion, envSet, err := motion.FromEnv()
	if err != nil {
		return nil, err
	}
	r := &Resolver{}
	ready := make(chan error, 1)
	go func() {
		th := theme.Default()
		// Fonts resolve meanwhile, off the window's path (Rust
		// PendingFonts).
		fonts := text.ResolveSystemWith(th.Faces)
		w, err := window.Open()
		if err != nil {
			discard(fonts)
			ready <- err
			return
		}
		defer w.Close()
		rend, err := render.New()
		if err != nil {
			discard(fonts)
			ready <- err
			return
		}
		defer rend.Close()
		if f, err := fonts.Wait(); err != nil {
			_, _ = fmt.Fprintf(log, "logis: %v; the cogitator stays mute\n", err)
		} else {
			rend.SetFonts(f) // the renderer owns them now
		}
		// Keys, the pointer and pasted text share one queue, in order.
		var actions []input.Action
		sink := func(a input.Action) { actions = append(actions, a) }
		kb := input.NewKeyboard(input.ComposeFromEnv())
		kb.SetLog(log)
		kb.SetSink(sink)
		w.SetKeyboard(kb)
		w.SetSink(sink)
		u := newUI(host, w, &th, kb)
		// The output a panel lands on re-fits it (wear.go).
		w.SetRoomSink(u.reroom)
		if envSet {
			u.envMotion = &envMotion
		}
		u.wearMotion()
		u.draw = func(sc *render.Scene, g window.Geometry) {
			rend.Draw(render.Target{Size: g.Physical, Scale: g.Scale}, sc)
		}
		u.measure = func(g window.Geometry) scene.Measure {
			return scene.MeasureFunc(func(t render.Text) (float32, bool) { return rend.MeasureText(t, g.Scale) })
		}
		u.setFaces, u.clock = rend.SetFaces, trace.MonotonicNS
		u.logf = func(message string) { _, _ = fmt.Fprintf(log, "logis: %s\n", message) }
		rend.SetLog(u.logf) // a custom shader that is skipped
		u.iconTheme = func(name string) {
			cache := icons.NewCache(icons.LoadThemes(icons.FromEnv(), name))
			u.icons = func(g window.Geometry) scene.Icons {
				return &icons.FrameIcons{Cache: cache, Atlas: rend.Atlas(), Scale: g.Scale}
			}
		}
		r.mu.Lock()
		r.wake = w.Wake
		r.mu.Unlock()
		ready <- nil
		err = r.run(w, u, &actions)
		host.Exit(2, "the noosphere fell silent: the Wayland connection was lost ("+reason(err)+
			"). The machine spirit departs; the next summons will rouse it anew.")
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return r, nil
}

// run serves commands, keys, the pointer and pastes until the compositor
// is lost.
func (r *Resolver) run(w *window.Window, u *ui, actions *[]input.Action) error {
	for {
		busy := false
		for _, command := range r.drain() {
			command(u)
			busy = true
		}
		for len(*actions) > 0 {
			a := (*actions)[0]
			*actions = (*actions)[1:]
			u.act(a)
			busy = true
		}
		u.expire()
		u.wake()
		u.check()
		// Something changed: draw now if a frame is due, else sleep in
		// poll until the compositor, a command, the toast's end or the
		// glitch's next start.
		timeout := time.Duration(-1)
		if until, ok := u.deadline(); ok {
			timeout = max(time.Until(until), 0)
		}
		if busy {
			timeout = 0
		}
		if err := w.Dispatch(timeout); err != nil {
			return err
		}
	}
}

func reason(err error) string {
	var lost *window.DisconnectedError
	if errors.As(err, &lost) {
		return lost.Reason
	}
	return err.Error()
}

// discard waits for fonts nobody will draw with and releases them.
func discard(p *text.PendingFonts) {
	if f, err := p.Wait(); err == nil {
		f.Close()
	}
}

func (r *Resolver) post(command func(*ui)) {
	r.mu.Lock()
	r.queue = append(r.queue, command)
	wake := r.wake
	r.mu.Unlock()
	if wake != nil {
		wake()
	}
}

func (r *Resolver) drain() []func(*ui) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queue
	r.queue = nil
	return q
}

// Accepts dmenu, provider and app sessions.
func (r *Resolver) Accepts(m protocol.Mode) error { return accepts(m) }

// Show shows a session's first stage.
func (r *Resolver) Show(s daemon.Session) { r.post(func(u *ui) { u.show(s) }) }

// Step applies what the shown session's provider asks next.
func (r *Resolver) Step(id daemon.SessionID, step provider.Step) {
	r.post(func(u *ui) { u.step(id, step) })
}

// End: the list needs no word that the input ended.
func (r *Resolver) End(daemon.SessionID) {}

// Hide takes a session away.
func (r *Resolver) Hide(id daemon.SessionID) { r.post(func(u *ui) { u.hide(id) }) }

// Linger keeps a session that is over in view on its toast, without the
// keyboard, until Hide.
func (r *Resolver) Linger(id daemon.SessionID) { r.post(func(u *ui) { u.linger(id) }) }

// Untoast takes the shown session's toast away.
func (r *Resolver) Untoast(id daemon.SessionID) { r.post(func(u *ui) { u.untoast(id) }) }

// Configure applies the config in force.
func (r *Resolver) Configure(c config.Config) { r.post(func(u *ui) { u.configure(c) }) }

// Toast shows a notice over the visible list.
func (r *Resolver) Toast(t daemon.Toast) { r.post(func(u *ui) { u.toast(t) }) }

// Activation asks the compositor for a token for the shown session's
// launch of appID; it reaches the daemon through the Host.
func (r *Resolver) Activation(id daemon.SessionID, appID string) {
	r.post(func(u *ui) { u.activation(id, appID) })
}

// Retheme wears t from now on, the visible panel included.
func (r *Resolver) Retheme(t *theme.Theme) { r.post(func(u *ui) { u.retheme(t) }) }
