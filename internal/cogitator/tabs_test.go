package cogitator

import (
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/render"
)

// Plain logis's tabbed launcher in the window (feat/logis-go 8cba2fc9, new
// beside Rust v1): the applications and the rites as two tabs under a
// strip; Tab and Shift+Tab (or a click on a plate) switch, the query stays
// across them, both plates count its matches; each tab's answers say
// which tab they come from.

// showTabbed shows plain logis's session id on tab with apps, and stages
// the rites in their tab.
func (r *rig) showTabbed(id daemon.SessionID, tab provider.Tab, apps []string, rites ...string) {
	o := provider.AppsOptions()
	r.ui.show(daemon.Session{ID: id, Mode: protocol.ModeApps, Options: o, Entries: entries(apps...), Tabbed: true, Tab: tab})
	prompt := "rite: "
	ro := list.DefaultOptions()
	ro.Prompt, ro.Placeholder = prompt, "choose a rite"
	r.ui.step(id, provider.Step{Kind: provider.StepStage, Options: ro, Entries: entries(rites...), Tab: provider.TabRites})
}

var (
	someApps  = []string{"kitty", "firefox", "keepassxc", "kate"}
	someRites = []string{"keyboard-layout", "night-light"}
)

func TestATabbedSessionOpensOnItsTabUnderTheStrip(t *testing.T) {
	r := newRig()
	r.showTabbed(1, provider.TabRites, someApps, someRites...)
	th := r.ui.theme
	want, _ := scene.FitTabbed(provider.AppsOptions(), th, 0)
	if r.surf.size != want {
		t.Fatalf("size %v, want %v", r.surf.size, want)
	}
	r.saw(t, "show 1", "items 4", "tab rites", "rites stage 1", "rites items 2")
	// The rites' first stage keeps the shared query and its own words:
	// the default prompt (the theme's label stands for it) and "seek a
	// rite".
	if l := r.ui.look(); l.Input != "seek a rite" || l.Prompt != list.DefaultOptions().Prompt || len(l.Rows) != 2 {
		t.Errorf("the rites: %+v", l)
	}
}

func TestTabAndShiftTabSwitchTabsAndKeepTheQuery(t *testing.T) {
	r := newRig()
	r.showTabbed(1, provider.TabApps, someApps, someRites...)
	r.events = nil
	r.act(input.Insert("ke"))
	if got := r.ui.look().Rows; len(got) != 2 {
		t.Fatalf("apps for ke: %+v", got)
	}
	r.act(input.ActTabNext)
	r.saw(t, "tab rites")
	if l := r.ui.look(); r.ui.shown.view.Query() != "ke" || len(l.Rows) != 1 || l.Rows[0].Text != "keyboard-layout" {
		t.Fatalf("the rites for ke: %+v", l)
	}
	r.act(input.ActTabNext) // two tabs: Tab goes back
	r.saw(t, "tab applications")
	r.act(input.ActTabPrevious)
	if r.ui.shown.tab != provider.TabRites {
		t.Fatal("Shift+Tab did not switch")
	}
	r.act(input.ActTabPrevious)
	// Down still moves the selection.
	r.events = nil
	r.act(input.ActNext)
	if len(r.events) != 2 || r.events[1] == "select none" || r.ui.shown.tab != provider.TabApps {
		t.Fatalf("Down: %q", r.events)
	}
	// Both plates count the query's matches.
	if st := r.ui.stripOf(r.ui.shown); st.Tabs[0].Count != 2 || st.Tabs[1].Count != 1 || st.Active != 0 {
		t.Errorf("strip %+v", st)
	}
}

func TestAnswersSayTheirTab(t *testing.T) {
	r := newRig()
	r.showTabbed(1, provider.TabApps, someApps, someRites...)
	r.act(input.ActTabNext, input.ActNext, input.ActAccept)
	want := answered{1, daemon.Answer{Kind: daemon.AnswerItem, Index: 1, Tab: provider.TabRites}}
	if len(r.host.answers) != 1 || r.host.answers[0] != want {
		t.Fatalf("%+v", r.host.answers)
	}
	// The rites wait for their provider; the applications do not.
	r.act(input.ActAccept)
	r.act(input.ActTabNext, input.ActAccept)
	if len(r.host.answers) != 2 || r.host.answers[1].answer.Tab != provider.TabApps {
		t.Fatalf("%+v", r.host.answers)
	}
	r.act(input.ActDismiss)
	if last := r.host.answers[len(r.host.answers)-1]; last.answer.Kind != daemon.AnswerDismissed || last.answer.Tab != provider.TabApps {
		t.Fatalf("%+v", last)
	}
}

func TestTheRitesStepsReachTheirTabWhicheverIsShown(t *testing.T) {
	r := newRig()
	r.showTabbed(1, provider.TabApps, someApps, someRites...)
	r.ui.step(1, provider.Step{Kind: provider.StepAppend, Entries: entries("vox"), Tab: provider.TabRites})
	if l := r.ui.look(); len(l.Rows) != 4 || l.Rows[0].Text != "kitty" {
		t.Fatalf("the applications changed: %+v", l.Rows)
	}
	if st := r.ui.stripOf(r.ui.shown); st.Tabs[1].Count != 3 {
		t.Errorf("rites count %d", st.Tabs[1].Count)
	}
	r.saw(t, "rites items 3")
}

func TestAClickOnAPlateSelectsItAndRowsAreFoundBelowTheStrip(t *testing.T) {
	r := newRig()
	r.showTabbed(1, provider.TabApps, someApps, someRites...)
	plates := scene.Plates(r.surf.size, r.ui.theme)
	p := plates[1].Rect
	r.events = nil
	r.ui.act(pointAt(input.PointPressed, float64(p[0]+p[2]/2), float64(p[1]+p[3]/2)))
	r.ui.act(pointAt(input.PointReleased, float64(p[0]+p[2]/2), float64(p[1]+p[3]/2)))
	if r.ui.shown.tab != provider.TabRites || !slices.Contains(r.events, "tab rites") {
		t.Fatalf("tab %v, %q", r.ui.shown.tab, r.events)
	}
	if len(r.host.answers) != 0 {
		t.Fatalf("the click answered: %+v", r.host.answers)
	}
	// A click on the lit plate changes nothing.
	r.events = nil
	r.ui.act(pointAt(input.PointPressed, float64(p[0]+4), float64(p[1]+4)))
	if len(r.events) != 0 {
		t.Errorf("%q", r.events)
	}
	g := scene.TabbedGrid(r.surf.size, r.ui.shown.rows, r.ui.theme)
	r.ui.act(pointAt(input.PointMoved, float64(g.Left+g.Right)/2, float64(g.Top(1)+g.Pitch/2)))
	r.saw(t, "select 1")
}

func TestUntabbedTabStillMovesTheSelection(t *testing.T) {
	r := newRig()
	r.show(1, list.DefaultOptions(), "alpha", "beta")
	r.act(input.ActTabNext)
	r.saw(t, "select 1")
	if r.ui.shown.tabbed {
		t.Fatal("a dmenu session is tabbed")
	}
}

func TestTheTabbedPanelDrawsTheStripAndFiresTheTabBurst(t *testing.T) {
	r := glitchRig(func(g *effects.Glitch) { g.Bursts.OpenMS, g.Bursts.TabMS = 0, 240 })
	r.showTabbed(1, provider.TabApps, someApps, someRites...)
	sc, _ := r.frame(1000, motion.Rest)
	plates := scene.Plates(r.surf.size, r.ui.theme)
	if !slices.Equal(sc.Post.Regions, plates[:]) {
		t.Fatalf("regions %v", sc.Post.Regions)
	}
	if !hasText(sc, "APPLICATIONS") || !hasText(sc, "RITES") {
		t.Fatal("no plates drawn")
	}
	r.ui.clock = func() uint64 { return 1050 * msNS }
	r.act(input.ActTabNext)
	if sc, d := r.frame(1100, motion.Rest); sc.Post.Moment.Event == 0 || !d.Moving {
		t.Errorf("no tab burst: %+v", sc.Post.Moment)
	}
	// An empty tab points to the other.
	r.act(input.Insert("firefox"))
	r.frame(2000, motion.Rest)
	if sc, _ := r.frame(2100, motion.Rest); !hasText(sc, "nothing answers here · 1 among the applications — Tab") {
		t.Errorf("%v", sceneTexts(sc))
	}
}

func sceneTexts(sc *render.Scene) []string {
	var out []string
	for _, x := range sc.Texts {
		out = append(out, x.Text)
	}
	return out
}

// Silent rites (2aaad4b9): plain logis whose caller found no servitor
// shows the strip all the same; the rites' plate counts "—", their tab
// says why, Return there does nothing, and Tab still switches.
func TestSilentRitesShowTheStripSayWhyAndTakeNoAnswer(t *testing.T) {
	r := newMovingRig()
	r.ui.show(daemon.Session{ID: 1, Mode: protocol.ModeApps, Options: provider.AppsOptions(),
		Entries: entries(someApps...), Tabbed: true, Tab: provider.TabRites, RitesSilent: true})
	r.saw(t, "show 1", "items 4", "tab rites", "rites silent")
	if want, _ := scene.FitTabbed(provider.AppsOptions(), r.ui.theme, 0); r.surf.size != want {
		t.Fatalf("size %v, want %v", r.surf.size, want)
	}
	st := r.ui.stripOf(r.ui.shown)
	if !st.Tabs[1].Silent || st.Tabs[0].Silent || st.Tabs[0].Count != 4 || st.Active != 1 {
		t.Fatalf("strip %+v", st)
	}
	if l := r.ui.look(); l.Input != "seek a rite" || len(l.Rows) != 0 {
		t.Errorf("the rites: %+v", l)
	}
	sc, _ := r.frame(1000, motion.Rest)
	if !hasText(sc, scene.SilentRites) || !hasText(sc, scene.SilentRitesWhy[0]) || !hasText(sc, "—") {
		t.Fatalf("%v", sceneTexts(sc))
	}
	// Return, Shift+Return and a typed query's Return do nothing there.
	r.events = nil
	r.act(input.ActAccept, input.ActDescend, input.Insert("kit"), input.ActAccept)
	if len(r.host.answers) != 0 {
		t.Fatalf("silent rites answered %+v", r.host.answers)
	}
	for _, e := range r.events {
		if strings.HasPrefix(e, "accept") || strings.HasPrefix(e, "typed") || strings.HasPrefix(e, "descend") {
			t.Fatalf("%q", r.events)
		}
	}
	// Tab still switches, the query with it, and the applications answer.
	r.act(input.ActTabNext)
	r.saw(t, "tab applications")
	r.act(input.ActAccept)
	if len(r.host.answers) != 1 || r.host.answers[0].answer.Tab != provider.TabApps || r.host.answers[0].answer.Kind != daemon.AnswerItem {
		t.Fatalf("%+v", r.host.answers)
	}
}

// A dismissal from silent rites says their tab, so it is remembered.
func TestEscapeFromSilentRitesSaysTheirTab(t *testing.T) {
	r := newRig()
	r.ui.show(daemon.Session{ID: 1, Mode: protocol.ModeApps, Options: provider.AppsOptions(),
		Entries: entries(someApps...), Tabbed: true, Tab: provider.TabRites, RitesSilent: true})
	r.act(input.ActDismiss)
	want := answered{1, daemon.Answer{Kind: daemon.AnswerDismissed, Tab: provider.TabRites}}
	if len(r.host.answers) != 1 || r.host.answers[0] != want {
		t.Fatalf("%+v", r.host.answers)
	}
}
