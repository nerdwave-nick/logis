package cogitator

import (
	"errors"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
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
// so the window closes at once (the answer is sent first).
//
// With the test trace on it reports `show <session>`, `items <total>`,
// `query <JSON string>`, `select <item>` (or `select none`), `accept
// <item>`, `descend <item>`, `typed <JSON string>`, `dismiss`, `toast <JSON
// string>`, `untoast` and `hide <session>` (the daemon took it away),
// beside the window's own focus, key, commit and settled events.

// noticeFor is how long a toast from the daemon stays (Rust NOTICE).
const noticeFor = 3 * time.Second

// surface is what the session logic needs of the window.
type surface interface {
	Show(size scale.Size, paint window.Painter) error
	Hide()
	Visible() bool
	RequestRedraw()
	// Size is the panel's logical size once configured: the space
	// pointer positions are in.
	Size() (scale.Size, bool)
	// Paste reads a selection; its text comes back as an Insert edit.
	Paste(input.Selection)
}

// focusPolicy is the keyboard's focus-loss policy.
type focusPolicy interface{ SetFocusLoss(config.FocusLoss) }

// shown is the session on screen.
type shown struct {
	id   daemon.SessionID
	view *list.View
	// asked is the size its options asked for (the theme's when none).
	asked list.Options
	// rows is how many rows its panel was sized for.
	rows uint32
	// count is how many items it has had.
	count int
	// toast is a notice from the daemon over the list.
	toast *list.Notice
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
	emit func(format string, args ...any)
	now  func() time.Time
	// setFaces resolves a theme's faces in the renderer (nil: none to
	// resolve); logf writes a line to the daemon's log; clock is the
	// frame's time (CLOCK_MONOTONIC ns).
	setFaces func([]text.Face) error
	logf     func(string)
	clock    func() uint64
}

func newUI(host daemon.Host, surf surface, th *theme.Theme, kb focusPolicy) *ui {
	return &ui{host: host, surf: surf, theme: th, keyboard: kb, emit: trace.Emit, now: time.Now}
}

// accepts says which sessions the window serves: dmenu sessions; the
// others arrive with their tickets (feat/logis-go providers, apps).
func accepts(m protocol.Mode) error {
	switch m {
	case protocol.ModeDmenu:
		return nil
	case protocol.ModeServitor:
		return errors.New("the cogitator serves servitor mode as a provider session " +
			"(logis --servitor), never as a session of its own")
	}
	return errors.New("the cogitator cannot yet manifest " + string(m) + " sessions")
}

// show shows a session: its window sized by its options (or the theme),
// its items so far, its query and selection.
func (u *ui) show(s daemon.Session) {
	o, err := protocol.DecodeOptions(s.Options)
	if err != nil {
		u.host.Fail(s.ID, err.Error())
		return
	}
	asked := o
	size, rows := scene.Fit(o, u.theme)
	o.Lines = &rows
	sh := &shown{id: s.ID, view: list.NewView(o), asked: asked, rows: rows}
	u.shown = nil
	if err := u.surf.Show(size, u.painter(sh)); err != nil {
		u.host.Fail(s.ID, err.Error())
		return
	}
	u.shown = sh
	u.emit("show %d", s.ID)
	u.push(sh, s.Items)
}

// painter draws sh's list each frame in the theme worn then, at the
// frame's time: the theme's continuous effects (glow pulse, travelling
// highlight, flicker, a pending row's shimmer) move with it, and keep the
// window drawing on every frame callback while they do (scene.Moving).
// Transitions are the motion ticket's.
func (u *ui) painter(sh *shown) window.Painter {
	return func(g window.Geometry) bool {
		look := u.lookOf(sh)
		if u.draw != nil {
			var at uint64
			if u.clock != nil {
				at = u.clock()
			}
			u.draw(scene.Draw(&look, g.Logical, scene.Style{Theme: u.theme, At: at}), g)
		}
		return scene.Moving(&look, u.theme)
	}
}

// appendItems: more items for session id.
func (u *ui) appendItems(id daemon.SessionID, items []protocol.Item) {
	if sh := u.mine(id); sh != nil {
		u.push(sh, items)
	}
}

func (u *ui) push(sh *shown, items []protocol.Item) {
	entries := make([]list.Entry, len(items))
	for i, it := range items {
		entries[i] = list.Entry{Text: it.Text}
		if it.Match != nil {
			entries[i].MatchText, entries[i].HasMatchText = *it.Match, true
		}
	}
	sh.count += len(items)
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
// the window's to read (its text comes back as an Insert edit).
func (u *ui) act(a input.Action) {
	sh := u.shown
	if sh == nil {
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
	case r.Kind == list.Dismiss:
		u.emit("dismiss")
		answer = daemon.Answer{Kind: daemon.AnswerDismissed}
	case r.Kind == list.Accept && r.Choice.Kind == list.ChoseTyped:
		u.emit("typed %s", trace.Quote(r.Choice.Text))
		answer = daemon.Answer{Kind: daemon.AnswerTyped, Text: r.Choice.Text}
	case r.Kind == list.Accept:
		word := "accept"
		if r.Choice.Kind == list.ChoseDescend {
			word = "descend"
		}
		u.emit("%s %d", word, r.Choice.Index)
		answer = daemon.Answer{Kind: daemon.AnswerItem, Index: int(r.Choice.Index)}
	default:
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
		u.host.Answer(sh.id, daemon.Answer{Kind: daemon.AnswerDismissed})
	}
}

// configure applies the config: what losing keyboard focus does.
func (u *ui) configure(c config.Config) { u.keyboard.SetFocusLoss(c.FocusLoss) }

// toast shows a notice from the daemon over the visible list for a while.
func (u *ui) toast(t daemon.Toast) {
	sh := u.shown
	if sh == nil {
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

// deadline is when the toast goes, if one shows.
func (u *ui) deadline() (time.Time, bool) {
	return u.noticeUntil, !u.noticeUntil.IsZero()
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
