package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Plain logis's tabbed launcher in the core (feat/logis-go 8cba2fc9): an
// app session that names its rites (servitor mode's provider) shows the
// applications and the rites as two tabs of one window, opening on the tab
// last used; the rites' steps go to their tab, and each tab's answers go
// to its own feed.

// openTabbed opens plain logis's session id, its rites run by program.
func (r *providerRig) openTabbed(t *testing.T, id SessionID, program string) *wire {
	t.Helper()
	w := greet(t, r.core, id)
	open := protocol.Open(protocol.ModeApps, nil)
	open.Rites = &protocol.Launch{Argv: [][]byte{[]byte(program)}, Cwd: []byte("/")}
	r.core.receive(id, open, nil)
	return w
}

// tabbed is plain logis's session id with the index read and its rites
// staged.
func (r *providerRig) tabbed(t *testing.T, id SessionID, rites ...string) (*wire, *fakeRunning) {
	t.Helper()
	w := r.openTabbed(t, id, "logis-provide-servitor")
	wantReplies(t, w, protocol.Visible())
	f := r.fakes[id]
	if f == nil {
		t.Fatal("the rites' provider did not start with the session")
	}
	wantSeenLines(t, f, `{"type":"hello","protocol":1}`)
	r.say(id, provider.Hello{Protocol: 1})
	var items []provider.Item
	for _, i := range rites {
		items = append(items, wireItem(i))
	}
	r.say(id, provider.Stage{ID: "rite", Items: items})
	return w, f
}

func TestATabbedSessionShowsTheAppsAndStagesTheRitesInTheirTab(t *testing.T) {
	r := newProviderRig()
	r.core.indexed(appIndex(t))
	_, f := r.tabbed(t, 1, "dnd", "vox")
	log := r.seen.take()
	if len(log) != 3 || !hasPrefix(log, 0, "show 1 apps tabbed applications [Anoint,") ||
		log[1] != "rites:" || log[2] != "stage 1 [DND,VOX]" {
		t.Fatalf("%q", log)
	}
	// Later words of the rites go to their tab too.
	r.say(1, provider.Items{Items: []provider.Item{wireItem("theme")}})
	wantLog(t, r.seen, "rites:", "append 1 [THEME]")
	// An answer from the rites goes to their provider; one from the
	// applications launches.
	r.core.answered(1, Answer{Kind: AnswerItem, Index: 1, Tab: provider.TabRites})
	wantSeenLines(t, f, `{"type":"activate","stage":"rite","id":"vox"}`)
	r.core.answered(1, Answer{Kind: AnswerItem, Index: anointRow(t, r.core)})
	wantLog(t, r.seen, "activation 1 forge-anoint")
}

func TestTheRitesClosingEndTheTabbedSessionAsClosed(t *testing.T) {
	r := newProviderRig()
	r.core.indexed(appIndex(t))
	w, f := r.tabbed(t, 1, "dnd")
	r.core.answered(1, Answer{Kind: AnswerItem, Tab: provider.TabRites})
	r.say(1, provider.Close{})
	if got := outcome(t, w); got != protocol.Closed {
		t.Fatal(got)
	}
	wantSeenLines(t, f, `{"type":"activate","stage":"rite","id":"dnd"}`, "eof")
}

func TestOnlyAnAppSessionNamesItsRites(t *testing.T) {
	r := newProviderRig()
	w := greet(t, r.core, 1)
	open := protocol.Open(protocol.ModeDmenu, nil)
	open.Rites = &protocol.Launch{Argv: [][]byte{[]byte("x")}, Cwd: []byte("/")}
	r.core.receive(1, open, nil)
	if replies := w.take(); !isError(replies, "only plain logis's app session names its rites") {
		t.Fatalf("%+v", replies)
	}
}

func TestTheTabLastUsedIsRememberedForTheNextTabbedSession(t *testing.T) {
	dir := t.TempDir()
	r := newProviderRig()
	r.core.apps.tabPath = filepath.Join(dir, "logis", "tab")
	r.core.indexed(appIndex(t))
	w, _ := r.tabbed(t, 1, "dnd")
	r.seen.take()
	r.core.answered(1, Answer{Kind: AnswerDismissed, Tab: provider.TabRites})
	if got := outcome(t, w); got != protocol.Dismissed {
		t.Fatal(got)
	}
	if b, err := os.ReadFile(r.core.apps.tabPath); err != nil || string(b) != "rites\n" {
		t.Fatalf("%q, %v", b, err)
	}
	r.tabbed(t, 2, "dnd")
	if log := r.seen.take(); !hasPrefix(log, 1, "show 2 apps tabbed rites [") {
		t.Fatalf("%q", log)
	}
	if got := loadTab(r.core.apps.tabPath); got != provider.TabRites {
		t.Errorf("loaded %v", got)
	}
	// --apps neither shows tabs nor changes the remembered one.
	apps := openApps(t, r.core, 3)
	r.core.answered(3, Answer{Kind: AnswerDismissed})
	wantReplies(t, apps, protocol.Visible(), result(protocol.Dismissed))
	if log := r.seen.take(); !hasPrefix(log, 1, "show 3 apps [") {
		t.Fatalf("%q", log)
	}
	if got := loadTab(r.core.apps.tabPath); got != provider.TabRites {
		t.Errorf("after --apps: %v", got)
	}
}

func TestAMissingOrStrangeTabFileIsTheApplications(t *testing.T) {
	dir := t.TempDir()
	if got := loadTab(filepath.Join(dir, "none")); got != provider.TabApps {
		t.Errorf("missing: %v", got)
	}
	odd := filepath.Join(dir, "odd")
	if err := os.WriteFile(odd, []byte("sideways\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadTab(odd); got != provider.TabApps {
		t.Errorf("odd: %v", got)
	}
}

// openSilent opens plain logis's session id whose caller found no
// servitor.
func openSilent(t *testing.T, c *core, id SessionID) *wire {
	t.Helper()
	w := greet(t, c, id)
	open := protocol.Open(protocol.ModeApps, nil)
	open.RitesSilent = true
	c.receive(id, open, nil)
	return w
}

// Silent rites (2aaad4b9): plain logis without servitor is the tabbed
// launcher still; no provider runs, an answer from the rites' tab does
// nothing, and the tab is remembered as ever.
func TestSilentRitesAreATabbedSessionWithoutAProvider(t *testing.T) {
	dir := t.TempDir()
	r := newProviderRig()
	r.core.apps.tabPath = filepath.Join(dir, "logis", "tab")
	r.core.indexed(appIndex(t))
	w := openSilent(t, r.core, 1)
	wantReplies(t, w, protocol.Visible())
	if log := r.seen.take(); len(log) != 1 || !hasPrefix(log, 0, "show 1 apps tabbed applications silent [Anoint,") {
		t.Fatalf("%q", log)
	}
	if len(r.fakes) != 0 {
		t.Fatal("a provider started for silent rites")
	}
	for _, kind := range []AnswerKind{AnswerItem, AnswerTyped, AnswerDescend} {
		r.core.answered(1, Answer{Kind: kind, Tab: provider.TabRites})
	}
	wantReplies(t, w)
	if log := r.seen.take(); len(log) != 0 {
		t.Fatalf("an answer from silent rites did %q", log)
	}
	r.core.answered(1, Answer{Kind: AnswerDismissed, Tab: provider.TabRites})
	if got := outcome(t, w); got != protocol.Dismissed {
		t.Fatal(got)
	}
	if b, err := os.ReadFile(r.core.apps.tabPath); err != nil || string(b) != "rites\n" {
		t.Fatalf("%q, %v", b, err)
	}
	openSilent(t, r.core, 2)
	if log := r.seen.take(); !hasPrefix(log, 1, "show 2 apps tabbed rites silent [") {
		t.Fatalf("%q", log)
	}
}

func TestOnlyAnAppSessionWithoutRitesHasThemSilent(t *testing.T) {
	for _, open := range []protocol.Request{
		{Type: protocol.TypeOpen, Mode: protocol.ModeDmenu, RitesSilent: true},
		{Type: protocol.TypeOpen, Mode: protocol.ModeApps, RitesSilent: true,
			Rites: &protocol.Launch{Argv: [][]byte{[]byte("x")}, Cwd: []byte("/")}},
	} {
		r := newProviderRig()
		w := greet(t, r.core, 1)
		r.core.receive(1, open, nil)
		if replies := w.take(); len(replies) != 1 || replies[0].Type != protocol.TypeError {
			t.Errorf("%s with rites %v: %+v", open.Mode, open.Rites != nil, replies)
		}
	}
}
