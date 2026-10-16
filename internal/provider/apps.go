package provider

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/launch"
	"github.com/nerdwave-nick/logis/internal/list"
)

// The app launcher as a built-in provider (feat/logis stories 5–7, §4.7;
// Rust src/provider/apps.rs): the applications of the daemon's index as
// the first stage, most frecent first, matched by name, generic name,
// keywords and program; an app's desktop actions as its second stage.
//
//   - Return on an app launches it; Shift+Return on an app with actions
//     opens them (the app itself first, then each action), and Return
//     there launches the one chosen. Shift+Return on an app without
//     actions launches it.
//   - A launch the daemon could not carry out is answered with an error
//     toast, and the stage takes Return again.

// failedFor is how long a failed launch's toast shows.
const failedFor = 5 * time.Second

// AppsSaid is what the user's answer asks of the daemon: Steps for the
// window (the actions stage, or nothing), or, with Launch, the launch of
// app ID (whose launch counts for frecency).
type AppsSaid struct {
	Steps  []Step
	Launch *launch.Plan
	ID     string
}

// Apps is one apps session's stages.
type Apps struct {
	// ids are the first stage's desktop-file IDs, by row.
	ids []string
	// actionsOf is the app whose actions show (none: the first stage);
	// actions are their IDs by row, "" for the app itself.
	actionsOf string
	actions   []string
}

// AppsOptions are the first stage's options.
func AppsOptions() list.Options {
	o := list.DefaultOptions()
	o.Placeholder, o.OnlyMatch = "summon an application", true
	return o
}

// OpenApps is the first stage: every app of ix, the most frecent first (at
// now, seconds since the epoch), then by name.
func OpenApps(ix *apps.Index, f *apps.Frecency, now uint64) (*Apps, Step) {
	all := ix.Apps()
	boosts := make([]uint32, len(all))
	order := make([]int, len(all))
	for i := range all {
		boosts[i], order[i] = f.Boost(all[i].ID, now), i
	}
	// Stable: equal boosts keep the index's order by name.
	slices.SortStableFunc(order, func(a, b int) int { return int(boosts[b]) - int(boosts[a]) })
	a := &Apps{ids: make([]string, len(order))}
	entries := make([]list.Entry, len(order))
	for row, i := range order {
		a.ids[row] = all[i].ID
		entries[row] = appEntry(&all[i], boosts[i])
	}
	return a, Step{Kind: StepStage, Options: AppsOptions(), Entries: entries}
}

// Answer is what the user's answer asks; terminal runs Terminal=true apps.
func (a *Apps) Answer(answer Answer, ix *apps.Index, terminal []string) AppsSaid {
	if answer.Kind != AnswerItem && answer.Kind != AnswerDescend {
		return AppsSaid{}
	}
	listing := a.actionsOf == ""
	rows := a.ids
	if !listing {
		rows = a.actions
	}
	if answer.Index < 0 || answer.Index >= len(rows) {
		return AppsSaid{}
	}
	id, action := rows[answer.Index], ""
	if !listing {
		id, action = a.actionsOf, rows[answer.Index]
	}
	app, ok := ix.Get(id)
	if !ok {
		return AppsSaid{Steps: AppsFailed(id + " is gone from the census")}
	}
	if answer.Kind == AnswerDescend && listing && len(app.Entry.Actions) > 0 {
		var step Step
		a.actionsOf, a.actions, step = id, actionRows(app), actionsStage(app)
		return AppsSaid{Steps: []Step{step}}
	}
	plan, err := apps.PlanFor(app, action, terminal)
	if err != nil {
		return AppsSaid{Steps: AppsFailed(app.Entry.Name + " cannot be summoned: " + err.Error())}
	}
	return AppsSaid{Launch: &plan, ID: id}
}

// AppsFailed is a launch that failed, as the window shows it: an error
// toast, and Return taken again.
func AppsFailed(message string) []Step {
	return []Step{
		{Kind: StepToast, Notice: list.Notice{Text: message, Level: list.Error}, Span: failedFor},
		{Kind: StepReady},
	}
}

// appEntry is an app's row: its name, matched also by generic name,
// keywords and program, with its icon and frecency boost.
func appEntry(app *apps.App, boost uint32) list.Entry {
	e := &app.Entry
	var more []string
	for _, m := range []string{e.GenericName, strings.Join(e.Keywords, " "), program(e.Exec)} {
		if m != "" {
			more = append(more, m)
		}
	}
	entry := decorated(e.Name, e.Icon, boost)
	if len(more) > 0 {
		entry.MatchText, entry.HasMatchText = e.Name+"\t"+strings.Join(more, "\t"), true
	}
	return entry
}

// program is the name of the program an Exec runs ("" for none).
func program(x *apps.Exec) string {
	if x == nil {
		return ""
	}
	p := strings.TrimRight(x.Program(), "/")
	if p == "" || p == "." || p == ".." || strings.HasSuffix(p, "/.") || strings.HasSuffix(p, "/..") {
		return "" // Rust's Path::file_name has none
	}
	return filepath.Base(p)
}

func decorated(text, icon string, boost uint32) list.Entry {
	return list.Entry{Text: text, Decor: &list.Decor{Icon: icon, HasIcon: icon != "", Boost: boost}}
}

// actionRows are the IDs of an app's actions stage by row: "" for the app
// itself, then each action's.
func actionRows(app *apps.App) []string {
	rows := []string{""}
	for _, x := range app.Entry.Actions {
		rows = append(rows, x.ID)
	}
	return rows
}

// actionsStage is an app's actions stage: the app itself, then each
// action.
func actionsStage(app *apps.App) Step {
	e := &app.Entry
	entries := []list.Entry{decorated(e.Name, e.Icon, 0)}
	for _, x := range e.Actions {
		icon := x.Icon
		if icon == "" {
			icon = e.Icon
		}
		entries = append(entries, decorated(x.Name, icon, 0))
	}
	o := AppsOptions()
	o.Prompt, o.Placeholder = e.Name+": ", ""
	return Step{Kind: StepStage, Options: o, Entries: entries}
}
