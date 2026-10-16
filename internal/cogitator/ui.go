package cogitator

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/glitch"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The window's session logic (Rust src/daemon/window/ui.rs), on the
// window's goroutine: the session on screen, the keys' actions applied to
// its list, and its answer to the daemon. A dmenu answer ends its session,
// so the window closes at once (the answer is sent first); after a
// provider's answer the window stays and Return waits for the provider's
// next stage or ready (Escape still dismisses). Provider steps: steps.go.
//
// With the test trace on it reports `show <session>`, `stage <session>` (a
// provider's next stage), `items <total>`, `update <item>`, `remove
// <item>`, `query <JSON string>`, `select <item>` (or `select none`),
// `accept <item>`, `descend <item>`, `typed <JSON string>`, `dismiss`,
// `toast <JSON string>`, `untoast`, `ready`, `linger <session>` and `hide
// <session>` (the daemon took it away), beside the window's own focus, key,
// commit and settled events.

// noticeFor is how long a toast from the daemon stays (Rust NOTICE).
const noticeFor = 3 * time.Second

// surface is what the session logic needs of the window.
type surface interface {
	Show(size scale.Size, paint window.Painter) error
	Hide()
	// ReleaseKeyboard lets the keyboard go while the panel stays in view.
	ReleaseKeyboard()
	Visible() bool
	RequestRedraw()
	// Size is the panel's logical size once configured: the space
	// pointer positions are in.
	Size() (scale.Size, bool)
	// Paste reads a selection; its text comes back as an Insert edit.
	Paste(input.Selection)
	// RequestToken asks for an xdg-activation token for appID; done gets
	// it ("" for none).
	RequestToken(appID string, done func(token string))
}

// focusPolicy is the keyboard's focus-loss policy.
type focusPolicy interface{ SetFocusLoss(config.FocusLoss) }

// shown is the session on screen.
type shown struct {
	id   daemon.SessionID
	view *list.View
	// rows is how many rows its panel was sized for.
	rows uint32
	// count is how many items it has had.
	count int
	// columns are its list's columns as they grew; a new list (a
	// provider's next stage) starts them afresh.
	columns scene.Columns
	// toast is a notice over the list: from the daemon, or the provider's.
	toast *list.Notice
	// asked are the first stage's lines and width (nil: the theme's): the
	// window's size, which later stages keep.
	asked [2]*uint32
	// endsOnAnswer: a dmenu answer ends the session; the window goes at
	// once.
	endsOnAnswer bool
	// waiting: an answer went out; Return waits for the provider.
	waiting bool
	// lingering: the session is over; its toast is left to read.
	lingering bool
	// glide moves the selection band to the selected row.
	glide motion.Glide
	// glitch is its glitch's clock, from its first frame (glitch.go);
	// next is when it next starts to move (0: nothing to come), woke the
	// start the window last woke for.
	glitch     *glitch.Clock
	next, woke uint64
}

type ui struct {
	host     daemon.Host
	surf     surface
	theme    *theme.Theme
	keyboard focusPolicy
	shown    *shown
	// noticeUntil is when the toast goes, while one shows.
	noticeUntil time.Time
	// draw paints a frame's scene (nil: nothing is painted, as in tests).
	draw func(sc *render.Scene, g window.Geometry)
	// icons are a frame's icons (nil: none, rows show their glyphs).
	icons func(g window.Geometry) scene.Icons
	// measure measures a frame's text at its scale (nil: nothing is
	// measured, columns are estimated).
	measure func(g window.Geometry) scene.Measure
	// iconTheme loads the icon theme of that name (nil: none is loaded);
	// iconThemeName is the one loaded.
	iconTheme     func(name string)
	iconThemeName string
	emit          func(format string, args ...any)
	now           func() time.Time
	// setFaces resolves a theme's faces in the renderer (nil: none to
	// resolve); logf writes a line to the daemon's log; clock is the
	// frame's time (CLOCK_MONOTONIC ns).
	setFaces func([]text.Face) error
	logf     func(string)
	clock    func() uint64
	// envMotion is LOGIS_MOTION's motion, if set: it outranks the theme's
	// timings; blur is the config's (wear.go).
	envMotion *motion.Motion
	blur      bool
	// random draws the glitch's chances (glitch.go).
	random func() float64
	// room is the logical height of the output panels show on (0: not
	// known): the window's word at each show, and the compositor's when it
	// puts a panel on an output (wear.go).
	room uint32
}

func newUI(host daemon.Host, surf surface, th *theme.Theme, kb focusPolicy) *ui {
	return &ui{host: host, surf: surf, theme: th, keyboard: kb, emit: trace.Emit, now: time.Now, random: rand.Float64}
}

// accepts says which sessions the window serves: dmenu, provider and app
// sessions.
func accepts(m protocol.Mode) error {
	switch m {
	case protocol.ModeDmenu, protocol.ModeProvider, protocol.ModeApps:
		return nil
	case protocol.ModeServitor:
		return errors.New("the cogitator serves servitor mode as a provider session " +
			"(logis --servitor), never as a session of its own")
	}
	return errors.New("the cogitator cannot yet manifest " + string(m) + " sessions")
}

// show shows a session: its window sized by its first stage's options (or
// the theme), its items so far, its query and selection.
func (u *ui) show(s daemon.Session) {
	o := s.Options
	asked := [2]*uint32{o.Lines, o.Width}
	if r, ok := u.surf.(roomer); ok && r.Room() > 0 {
		u.room = r.Room()
	}
	size, rows := u.fitIn(o)
	o.Lines = &rows
	sh := &shown{id: s.ID, view: list.NewView(o), rows: rows, asked: asked, endsOnAnswer: s.Mode == protocol.ModeDmenu,
		glide: motion.NewGlide(u.motion().Selection), glitch: glitch.New(u.random)}
	u.shown = nil
	if err := u.present(size, sh); err != nil {
		u.host.Fail(s.ID, err.Error())
		return
	}
	u.shown = sh
	u.emit("show %d", s.ID)
	u.push(sh, s.Entries)
}

func (u *ui) push(sh *shown, entries []list.Entry) {
	sh.count += len(entries)
	if sh.view.Push(entries) {
		u.surf.RequestRedraw()
	}
	u.emit("items %d", sh.count)
}

// hide takes session id away (answered, cancelled or replaced).
func (u *ui) hide(id daemon.SessionID) {
	if u.mine(id) != nil {
		u.shown = nil
		u.surf.Hide()
		u.emit("hide %d", id)
	}
}

func (u *ui) mine(id daemon.SessionID) *shown {
	if u.shown != nil && u.shown.id == id {
		return u.shown
	}
	return nil
}

// act applies a key's or the pointer's action to the shown list. A point
// means the row drawn under it, on the panel's current size; a paste is
// the window's to read (its text comes back as an Insert edit). A
// lingering session takes none; after a provider's answer, Return waits
// for its next stage or ready.
func (u *ui) act(a input.Action) {
	sh := u.shown
	if sh == nil || sh.lingering {
		return
	}
	var r list.Reaction
	switch a.Kind {
	case input.Paste:
		u.surf.Paste(a.Selection)
		return
	case input.PointAt:
		size, ok := u.surf.Size()
		if !ok {
			return
		}
		r = sh.view.Point(a.Point, scene.GridOf(size, sh.rows, u.theme))
	default:
		r = sh.view.Apply(a)
	}
	var answer daemon.Answer
	switch {
	case r.Kind == list.Changed:
		u.emit("query %s", trace.Quote(sh.view.Query()))
		if item, ok := sh.view.Selected(); ok {
			u.emit("select %d", item)
		} else {
			u.emit("select none")
		}
		u.surf.RequestRedraw()
		return
	case r.Kind == list.Accept && sh.waiting:
		return
	case r.Kind == list.Dismiss:
		u.emit("dismiss")
		answer = daemon.Answer{Kind: daemon.AnswerDismissed}
	case r.Kind == list.Accept && r.Choice.Kind == list.ChoseTyped:
		u.emit("typed %s", trace.Quote(r.Choice.Text))
		answer = daemon.Answer{Kind: daemon.AnswerTyped, Text: r.Choice.Text}
	case r.Kind == list.Accept:
		word, kind := "accept", daemon.AnswerItem
		if r.Choice.Kind == list.ChoseDescend {
			word, kind = "descend", daemon.AnswerDescend
		}
		u.emit("%s %d", word, r.Choice.Index)
		answer = daemon.Answer{Kind: kind, Index: int(r.Choice.Index)}
	default:
		return
	}
	if !sh.endsOnAnswer && answer.Kind != daemon.AnswerDismissed {
		sh.waiting = true
		u.host.Answer(sh.id, answer)
		return
	}
	// The session ends: the daemon answers the client at once (and shows
	// the next session, if one waits), then the window closes.
	u.shown = nil
	u.host.Answer(sh.id, answer)
	u.surf.Hide()
}

// check dismisses the shown session once the compositor closed its
// surface (its output went away).
func (u *ui) check() {
	if sh := u.shown; sh != nil && !u.surf.Visible() {
		u.shown = nil
		if !sh.lingering {
			u.host.Answer(sh.id, daemon.Answer{Kind: daemon.AnswerDismissed})
		}
	}
}

// configure applies the config: what losing keyboard focus does, the
// icon theme rows' icons come from, and blur behind the body.
func (u *ui) configure(c config.Config) {
	u.keyboard.SetFocusLoss(c.FocusLoss)
	u.blur = c.Blur
	u.wearBlur()
	if u.iconTheme != nil && c.IconTheme != u.iconThemeName {
		u.iconThemeName = c.IconTheme
		u.iconTheme(c.IconTheme)
		u.surf.RequestRedraw()
	}
}

// toast shows a notice from the daemon over the visible list for a while.
func (u *ui) toast(t daemon.Toast) {
	sh := u.shown
	if sh == nil || sh.lingering {
		return
	}
	words := "the config is heretical: "
	if t.Kind == daemon.ToastThemeRejected {
		words = "the theme is heretical: "
	}
	n := &list.Notice{Text: words + t.Error, Level: list.Error}
	u.emit("toast %s", trace.Quote(n.Text))
	sh.toast = n
	u.noticeUntil = u.now().Add(noticeFor)
	u.surf.RequestRedraw()
}

// deadline is when the toast goes, if one shows, or when the glitch
// next starts to move, whichever comes first.
func (u *ui) deadline() (time.Time, bool) {
	at, ok := u.noticeUntil, !u.noticeUntil.IsZero()
	if next, wake := u.glitchWake(); wake {
		w := u.now().Add(time.Duration(next - min(next, u.clock())))
		if !ok || w.Before(at) {
			at, ok = w, true
		}
	}
	return at, ok
}

// expire takes the toast away once its time is up.
func (u *ui) expire() {
	if u.noticeUntil.IsZero() || u.now().Before(u.noticeUntil) {
		return
	}
	u.noticeUntil = time.Time{}
	if sh := u.shown; sh != nil && sh.toast != nil {
		sh.toast = nil
		u.surf.RequestRedraw()
		u.emit("untoast")
	}
}

// look is what the shown list draws now.
func (u *ui) look() list.Look {
	if u.shown == nil {
		return list.Look{}
	}
	return u.lookOf(u.shown)
}

func (u *ui) lookOf(sh *shown) list.Look {
	l := sh.view.Look()
	l.Toast = sh.toast
	return l
}
