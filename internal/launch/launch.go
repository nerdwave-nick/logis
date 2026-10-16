package launch

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/dbus"
)

// A Plan is what to launch.
type Plan struct {
	// AppID is the application ID: the desktop-file ID without .desktop.
	AppID string
	// Name is the translated Name, for the scope's description.
	Name string
	// Argv is the command line (field codes expanded, a terminal's in
	// front); empty for an application only D-Bus can start.
	Argv []string
	// Cwd is the working directory (Path); the home directory when "".
	Cwd string
	// DBus starts it through D-Bus (DBusActivatable=true) when the bus can.
	DBus bool
	// Action is the desktop action to activate; "" for the application.
	Action string
}

// A Session is the session to launch into.
type Session struct {
	// Bus is the session bus's address; "" if none.
	Bus string
	// Env is the environment when the bus cannot say, as NAME=VALUE
	// entries: the client's (fresh), never the daemon's own.
	Env []string
}

// Via is how an application was started.
type Via int

const (
	// Plain is a process outside any scope (no systemd user manager
	// answered).
	Plain Via = iota
	// Scope is a process in a transient systemd scope (Launched.Unit).
	Scope
	// Bus is org.freedesktop.Application on the session bus.
	Bus
)

func (v Via) String() string {
	switch v {
	case Plain:
		return "plain"
	case Scope:
		return "scope"
	case Bus:
		return "bus"
	}
	return fmt.Sprintf("Via(%d)", int(v))
}

// Launched is a launch that went ahead.
type Launched struct {
	Via Via
	// Unit is the scope's unit name (Via Scope).
	Unit string
	// PID is the process; 0 if D-Bus started it.
	PID int
	// Warnings are what went wrong on the way without stopping it (for the
	// log).
	Warnings []string
}

// A NotFoundError says the program is not found (not on the session's PATH).
type NotFoundError struct{ Program string }

func (e *NotFoundError) Error() string {
	return "no program '" + e.Program + "' answers the summons"
}

// A NotActivatableError says only D-Bus could start the application, and
// the bus cannot.
type NotActivatableError struct{ AppID string }

func (e *NotActivatableError) Error() string {
	return "the bus cannot wake '" + e.AppID + "' and it names no Exec"
}

// A SpawnError says the process could not be started.
type SpawnError struct{ Err error }

func (e *SpawnError) Error() string { return "the process would not start: " + e.Err.Error() }
func (e *SpawnError) Unwrap() error { return e.Err }

// ScopePatience is how long a launch waits for systemd to move a process
// into its scope.
const ScopePatience = 200 * time.Millisecond

// tokenVars are the variables that carry an activation token.
var tokenVars = [...]string{"XDG_ACTIVATION_TOKEN", "DESKTOP_STARTUP_ID"}

// Launch launches plan with the xdg-activation token the compositor gave
// ("" for none).
func Launch(plan Plan, token string, session Session) (Launched, error) {
	var warnings []string
	var bus *dbus.Conn
	if session.Bus != "" {
		c, err := dbus.Connect(session.Bus)
		if err != nil {
			warnings = append(warnings, "no session bus: "+err.Error())
		}
		bus = c
	}
	defer func() {
		if bus != nil {
			_ = bus.Close()
		}
	}()
	if plan.DBus {
		if bus != nil {
			if ok, err := Activatable(bus, plan.AppID); ok && err == nil {
				go activate(bus, plan, token)
				bus = nil // the activation owns it now
				return Launched{Via: Bus, Warnings: warnings}, nil
			}
		}
		if len(plan.Argv) == 0 {
			return Launched{}, &NotActivatableError{plan.AppID}
		}
		warnings = append(warnings, "the bus cannot activate "+plan.AppID+"; its Exec runs instead")
	}
	env := session.Env
	if bus != nil {
		e, err := Environment(bus)
		if err != nil {
			warnings = append(warnings, "no user manager's environment: "+err.Error())
		} else {
			env = e
		}
	}
	env = withToken(env, token)
	program := ""
	if len(plan.Argv) > 0 {
		program = plan.Argv[0]
	}
	found, ok := LookPath(lookup(env, "PATH"), program)
	if !ok {
		return Launched{}, &NotFoundError{program}
	}
	cwd := "/"
	for _, dir := range []string{plan.Cwd, lookup(env, "HOME")} {
		if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
			cwd = dir
			break
		}
	}
	s, err := start(found, plan.Argv, env, cwd, bus != nil)
	if err != nil {
		return Launched{}, &SpawnError{err}
	}
	launched := Launched{Via: Plain, PID: s.pid()}
	if bus != nil {
		unit := UnitName(plan.AppID, launched.PID)
		if err := StartScope(bus, unit, launched.PID, plan.Name); err != nil {
			warnings = append(warnings, "no scope: "+err.Error())
		} else {
			if !inUnit(launched.PID, unit, ScopePatience) {
				warnings = append(warnings, unit+" did not take the process in time")
			}
			launched.Via, launched.Unit = Scope, unit
		}
	}
	s.release()
	launched.Warnings = warnings
	return launched, nil
}

// withToken is env without any inherited activation token, with token's
// variables if there is one.
func withToken(env []string, token string) []string {
	out := make([]string, 0, len(env)+len(tokenVars))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name != tokenVars[0] && name != tokenVars[1] {
			out = append(out, kv)
		}
	}
	if token != "" {
		for _, name := range tokenVars {
			out = append(out, name+"="+token)
		}
	}
	return out
}

// lookup is the first value of variable name in env; "" if unset.
func lookup(env []string, name string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}

// activate activates a D-Bus application on a goroutine of its own (the
// application answers once it has started, which may take a while), then
// closes bus.
func activate(bus *dbus.Conn, plan Plan, token string) {
	defer func() { _ = bus.Close() }()
	if err := Activate(bus, plan.AppID, plan.Action, token); err != nil {
		// The daemon's stderr is its log; it may be gone.
		_, _ = fmt.Fprintf(os.Stderr, "logis: %s would not wake: %v\n", plan.AppID, err)
	}
}
