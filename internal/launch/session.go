package launch

// The calls a launch makes on the session bus:
//
//   - the systemd user manager's environment (Manager.Environment): the
//     session environment the compositor and the session scripts imported;
//   - a transient scope for a launched process (StartTransientUnit), named
//     by systemd's convention for applications
//     (app-<launcher>-<escaped app id>-<random>.scope, in app.slice);
//   - org.freedesktop.Application.Activate/ActivateAction for a
//     DBusActivatable application, with the activation token as platform
//     data; whether the bus can start it at all is asked first.

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nerdwave-nick/logis/internal/dbus"
)

const (
	systemd     = "org.freedesktop.systemd1"
	systemdPath = "/org/freedesktop/systemd1"
	manager     = "org.freedesktop.systemd1.Manager"
)

// Environment is the user manager's environment, as NAME=VALUE entries.
func Environment(bus *dbus.Conn) ([]string, error) {
	reply, err := bus.Call(dbus.Call{
		Destination: systemd,
		Path:        systemdPath,
		Interface:   "org.freedesktop.DBus.Properties",
		Member:      "Get",
		Body:        []dbus.Value{dbus.Str(manager), dbus.Str("Environment")},
	})
	if err != nil {
		return nil, err
	}
	unexpected := &dbus.RemoteError{Name: "logis.UnexpectedReply", Message: "Environment is not a list of strings"}
	if len(reply) == 0 {
		return nil, unexpected
	}
	v, ok := reply[0].(dbus.Variant)
	if !ok {
		return nil, unexpected
	}
	list, ok := v.Value.(dbus.Array)
	if !ok {
		return nil, unexpected
	}
	var env []string
	for _, item := range list.Items {
		if pair, ok := dbus.AsString(item); ok && strings.Contains(pair, "=") {
			env = append(env, pair)
		}
	}
	return env, nil
}

// UnitName is the scope unit for process pid of application appID.
func UnitName(appID string, pid int) string {
	return "app-logis-" + Escape(appID) + "-" + strconv.Itoa(pid) + ".scope"
}

// Escape is systemd's unit-name escaping (systemd-escape): letters, digits,
// ':', '_' and '.' (not leading) stay, '/' becomes '-', everything else
// \xNN.
func Escape(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '/':
			b.WriteByte('-')
		case c == '.' && i == 0:
			b.WriteString(`\x2e`)
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == ':', c == '_', c == '.':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

// StartScope starts a transient scope holding process pid.
func StartScope(bus *dbus.Conn, unit string, pid int, description string) error {
	property := func(name string, v dbus.Value) dbus.Value {
		return dbus.Struct{dbus.Str(name), dbus.Variant{Value: v}}
	}
	_, err := bus.Call(dbus.Call{
		Destination: systemd,
		Path:        systemdPath,
		Interface:   manager,
		Member:      "StartTransientUnit",
		Body: []dbus.Value{
			dbus.Str(unit),
			dbus.Str("fail"),
			dbus.Array{Elem: "(sv)", Items: []dbus.Value{
				property("PIDs", dbus.Array{Elem: "u", Items: []dbus.Value{dbus.U32(pid)}}),
				property("Slice", dbus.Str("app.slice")),
				property("Description", dbus.Str(description)),
				property("CollectMode", dbus.Str("inactive-or-failed")),
			}},
			dbus.Array{Elem: "(sa(sv))"},
		},
	})
	return err
}

// busDaemon is a call to the bus itself.
func busDaemon(member string, body ...dbus.Value) dbus.Call {
	return dbus.Call{
		Destination: "org.freedesktop.DBus",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Member:      member,
		Body:        body,
	}
}

// Activatable says whether the bus can reach name: it runs, or the bus can
// start it.
func Activatable(bus *dbus.Conn, name string) (bool, error) {
	owned, err := bus.Call(busDaemon("NameHasOwner", dbus.Str(name)))
	if err != nil {
		return false, err
	}
	if len(owned) > 0 && owned[0] == dbus.Bool(true) {
		return true, nil
	}
	names, err := bus.Call(busDaemon("ListActivatableNames"))
	if err != nil {
		return false, err
	}
	if len(names) == 0 {
		return false, nil
	}
	all, ok := names[0].(dbus.Array)
	return ok && slices.Contains(all.Items, dbus.Value(dbus.Str(name))), nil
}

// ObjectPath is the object path of application appID (org.gnome.Nautilus →
// /org/gnome/Nautilus; '-' becomes '_').
func ObjectPath(appID string) string {
	return "/" + strings.NewReplacer(".", "/", "-", "_").Replace(appID)
}

// Activate activates application appID (or its action, if not ""), waiting
// for its reply; token ("" for none) goes along as platform data.
func Activate(bus *dbus.Conn, appID, action, token string) error {
	data := dbus.Array{Elem: "{sv}"}
	if token != "" {
		for _, key := range []string{"activation-token", "desktop-startup-id"} {
			data.Items = append(data.Items, dbus.Entry{Key: dbus.Str(key), Value: dbus.Variant{Value: dbus.Str(token)}})
		}
	}
	member, body := "Activate", []dbus.Value{data}
	if action != "" {
		member, body = "ActivateAction", []dbus.Value{dbus.Str(action), dbus.Array{Elem: "v"}, data}
	}
	_, err := bus.Call(dbus.Call{
		Destination: appID,
		Path:        ObjectPath(appID),
		Interface:   "org.freedesktop.Application",
		Member:      member,
		Body:        body,
	})
	return err
}

// EnvFromBytes keeps the NAME=VALUE entries of an environment received as
// bytes (a client's), dropping any without '='.
func EnvFromBytes(pairs [][]byte) []string {
	var env []string
	for _, pair := range pairs {
		if bytes.IndexByte(pair, '=') >= 0 {
			env = append(env, string(pair))
		}
	}
	return env
}
