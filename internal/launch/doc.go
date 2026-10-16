// Package launch starts desktop applications (spec §3, §4.7): in the
// session environment, into a transient systemd user scope where one is
// available, with an xdg-activation token, detached so they outlive logis;
// DBusActivatable applications through org.freedesktop.Application.
//
// # Why D-Bus directly, not systemd-run --user --scope
//
// Both put the process into a transient scope. logis needs a session-bus
// client anyway, for DBusActivatable entries and for the session
// environment (Manager.Environment), and then one more call
// (StartTransientUnit) does what systemd-run would: no extra process per
// launch, no dependency on the systemd-run binary, and a missing or
// refusing user manager is known before the application starts, so logis
// can still start it outside a scope ("where available"). The process is
// held until systemd has moved it (see start), so it and everything it
// forks lives in its scope, never in the daemon's cgroup (which
// systemctl --user stop logis would end).
//
// # The session environment
//
// The user manager's environment is the session's: what the compositor and
// the session scripts imported (WAYLAND_DISPLAY, XDG_CURRENT_DESKTOP …),
// kept current by them. Without a user manager, the environment of the
// client that asked for the launch is used: fresh from the user's session.
// The daemon's own environment, frozen at its start, is never used. A new
// activation token replaces any inherited one; without one, none is passed
// on.
package launch
