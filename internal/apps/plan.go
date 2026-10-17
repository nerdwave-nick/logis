package apps

import (
	"slices"

	"github.com/nerdwave-nick/logis/internal/launch"
)

// What launching an application (or one of its desktop actions) runs: its
// Exec with the field codes expanded, behind the configured terminal for
// Terminal=true, in its Path; or, for DBusActivatable=true, an activation
// the bus may perform (with the Exec, if any, to fall back on).

// Terminal finds the program and leading arguments that run a command in a
// terminal (["foot", "-e"]); it is asked once for each launch of a
// Terminal=true app, and only then. Its error says why there is none.
type Terminal func() ([]string, error)

// A NoTerminalError says a Terminal=true app found no terminal to run in,
// and how to name one: in Config, the config file the daemon reads ("" if
// it has none).
type NoTerminalError struct{ Config string }

func (e *NoTerminalError) Error() string {
	where := "the config"
	if e.Config != "" {
		where = e.Config
	}
	return `it must run in a terminal, and no terminal was found. ` +
		`Set "terminal" in ` + where + `, e.g. "terminal": ["foot", "-e"]`
}

// NoActionError: the application has no such desktop action.
type NoActionError struct{ ID string }

func (e *NoActionError) Error() string { return "it knows no action '" + e.ID + "'" }

// PlanFor is the plan for app, or its action ("" for the app itself).
// terminal finds the terminal a Terminal=true app runs in; nil finds none.
func PlanFor(app *App, action string, terminal Terminal) (launch.Plan, error) {
	e := &app.Entry
	exec, icon, name := e.Exec, e.Icon, e.Name
	if action != "" {
		i := slices.IndexFunc(e.Actions, func(a Action) bool { return a.ID == action })
		if i < 0 {
			return launch.Plan{}, &NoActionError{ID: action}
		}
		a := e.Actions[i]
		exec, name = a.Exec, e.Name+": "+a.Name
		if a.Icon != "" {
			icon = a.Icon
		}
	}
	var argv []string
	if exec != nil {
		argv = exec.Expand(Fields{Icon: icon, Name: e.Name, Location: app.Location})
	}
	if e.Terminal && len(argv) > 0 {
		if terminal == nil {
			return launch.Plan{}, &NoTerminalError{}
		}
		prefix, err := terminal()
		if err != nil {
			return launch.Plan{}, err
		}
		argv = append(slices.Clone(prefix), argv...)
	}
	return launch.Plan{
		AppID:  app.AppID(),
		Name:   name,
		Argv:   argv,
		Cwd:    e.Path,
		DBus:   e.DBusActivatable,
		Action: action,
	}, nil
}
