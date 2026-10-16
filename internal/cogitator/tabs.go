package cogitator

import (
	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Plain logis's tabbed launcher in the window (feat/logis-go 8cba2fc9, new
// beside Rust v1): the applications and the rites, each a pane of the one
// shown session, under the tab strip. The plates stay in place; Tab and
// Shift+Tab (both: there are two tabs) or a press on the other plate only
// move which is shown, and fire the glitch's tab burst. The query stays
// across the tabs: an edit is copied to the other pane, so both plates
// count its matches. Up, Down, Ctrl+N and Ctrl+P move the selection as
// ever; Return answers the shown tab, and every answer says its tab. The
// rites' first stage keeps the query and says "seek a rite" under the
// theme's own label. With the test trace on it reports `tab <name>` when a
// tabbed panel shows and on each switch, and the rites' steps as `rites
// stage <session>`, `rites items <n>` and so on.

// pane is one list of a shown session: the only one, or a tab's.
type pane struct {
	view *list.View
	// count is how many items it has had.
	count int
	// columns are its list's columns as they grew; a new list (a
	// provider's next stage) starts them afresh.
	columns scene.Columns
	// waiting: an answer went out; Return waits for the provider.
	waiting bool
	// staged: its provider's first stage came (the rites').
	staged bool
}

// tabNames are the tabs' names, by provider.Tab: the trace's and the
// plates' words.
var tabNames = [2]string{"applications", "rites"}

// ritesPlaceholder is the rites' tab's word for an empty query.
const ritesPlaceholder = "seek a rite"

// openTabs makes sh plain logis's tabbed launcher, showing tab: the
// rites' pane waits for its provider's first stage.
func openTabs(sh *shown, tab provider.Tab) {
	o := list.DefaultOptions()
	o.Placeholder, o.Lines = ritesPlaceholder, &sh.rows
	sh.tabbed, sh.panes[1] = true, &pane{view: list.NewView(o)}
	if int(tab) < len(sh.panes) {
		sh.tab, sh.pane = tab, sh.panes[tab]
	}
}

// paneOf is the pane a step for tab goes to: the tab's in a tabbed
// session, else the only one.
func (sh *shown) paneOf(tab provider.Tab) *pane {
	if sh.tabbed && int(tab) < len(sh.panes) {
		return sh.panes[tab]
	}
	return sh.pane
}

// firstRites is o for p's first stage when p is the rites': the default
// prompt (the theme's label stands for it), "seek a rite", and the query
// typed so far.
func firstRites(sh *shown, p *pane, o list.Options) list.Options {
	if p != sh.panes[provider.TabRites] || p.staged {
		return o
	}
	p.staged = true
	o.Prompt, o.Placeholder = list.DefaultOptions().Prompt, ritesPlaceholder
	if o.Search == "" {
		o.Search = sh.panes[provider.TabApps].view.Query()
	}
	return o
}

// tabAct takes what switches tabs: Tab and Shift+Tab, a press on the
// other plate (a press on the lit one does nothing). False for anything
// else.
func (u *ui) tabAct(sh *shown, a input.Action) bool {
	switch {
	case a.Kind == input.Select && a.ByTab:
		u.switchTab(sh, 1-sh.tab)
		return true
	case a.Kind == input.PointAt && a.Point.Kind == input.PointPressed:
		size, ok := u.surf.Size()
		if !ok {
			return false
		}
		i, on := scene.PlateAt(a.Point.X, a.Point.Y, size, u.theme)
		if on {
			u.switchTab(sh, provider.Tab(i))
		}
		return on
	}
	return false
}

// switchTab shows tab, with the query typed so far, and fires the glitch's
// tab burst.
func (u *ui) switchTab(sh *shown, tab provider.Tab) {
	if tab == sh.tab {
		return
	}
	query := sh.view.Query()
	sh.tab, sh.pane = tab, sh.panes[tab]
	sh.view.SetQuery(query)
	sh.glide.Reset()
	u.emit("tab %s", tabNames[tab])
	u.glitchBurst(effects.EventTab)
	u.surf.RequestRedraw()
}

// mirrorQuery copies the shown pane's query to the other tab's.
func mirrorQuery(sh *shown) {
	if !sh.tabbed {
		return
	}
	for _, p := range sh.panes {
		if p != sh.pane {
			p.view.SetQuery(sh.view.Query())
		}
	}
}

// stripOf is sh's tab strip now: its plates with the matches of the
// query, and whether the shown tab has none for it.
func (u *ui) stripOf(sh *shown) scene.Strip {
	st := scene.Strip{Active: int(sh.tab)}
	icons := [2]scene.TabIcon{scene.AppsIcon, scene.RitesIcon}
	for i, p := range sh.panes {
		st.Tabs[i] = scene.Tab{Label: tabNames[i], Count: p.view.Matched(), Icon: icons[i]}
	}
	st.Empty = sh.view.Matched() == 0 && (sh.view.Query() != "" || sh.view.Total() > 0)
	return st
}

// grid is where sh's rows lie on a surface of size: under the strip when
// tabbed.
func (u *ui) grid(sh *shown, size scale.Size) scene.Grid {
	if sh.tabbed {
		return scene.TabbedGrid(size, sh.rows, u.theme)
	}
	return scene.GridOf(size, sh.rows, u.theme)
}

// bandTop is where sh's selection band rests on a surface of size.
func (u *ui) bandTop(sh *shown, l *list.Look, size scale.Size) (float32, bool) {
	if sh.tabbed {
		return scene.TabbedBandTop(l, size, u.theme)
	}
	return scene.BandTop(l, size, u.theme)
}

// body is sh's panel's body on a surface of size, and its radius.
func (u *ui) body(sh *shown, size scale.Size) ([4]float32, float32) {
	if sh.tabbed {
		return scene.TabbedBody(size, u.theme)
	}
	return scene.Body(size, u.theme)
}
