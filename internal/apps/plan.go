package apps

import (
	"errors"
	"slices"

	"github.com/nerdwave-nick/logis/internal/launch"
)

// What launching an application (or one of its desktop actions) runs: its
// Exec with the field codes expanded, behind the configured terminal for
// Terminal=true, in its Path; or, for DBusActivatable=true, an activation
// the bus may perform (with the Exec, if any, to fall back on).

// ErrNoTerminal: Terminal=true, and no terminal is configured.
var ErrNoTerminal = errors.New(`it must run in a terminal, and none is named ` +
	`(set "terminal" in the config, or $TERMINAL)`)

// NoActionError: the application has no such desktop action.
type NoActionError struct{ ID string }

func (e *NoActionError) Error() string { return "it knows no action '" + e.ID + "'" }

// PlanFor is the plan for app, or its action ("" for the app itself).
// terminal is the program and leading arguments that run a command in a
// terminal (["foot", "-e"]), empty if none is known.
func PlanFor(app *App, action string, terminal []string) (launch.Plan, error) {
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
		if len(terminal) == 0 {
			return launch.Plan{}, ErrNoTerminal
		}
		argv = append(slices.Clone(terminal), argv...)
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
