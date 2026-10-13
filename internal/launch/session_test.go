package launch

import (
	"errors"
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/dbus"
	"github.com/nerdwave-nick/logis/internal/dbus/dbustest"
)

func TestUnitNamesEscapeTheApplicationID(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{UnitName("forge-anoint", 42), `app-logis-forge\x2danoint-42.scope`},
		{UnitName("org.gnome.Nautilus", 7), "app-logis-org.gnome.Nautilus-7.scope"},
		{Escape(".hidden/x y"), `\x2ehidden-x\x20y`},
		{Escape("a:b_c.d9"), "a:b_c.d9"},
		{Escape("ä"), `\xc3\xa4`},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q; want %q", tc.got, tc.want)
		}
	}
}

func TestObjectPathsFollowTheApplicationID(t *testing.T) {
	if got := ObjectPath("org.gnome.Nautilus"); got != "/org/gnome/Nautilus" {
		t.Errorf("ObjectPath = %q", got)
	}
	if got := ObjectPath("org.forge.holy-oil"); got != "/org/forge/holy_oil" {
		t.Errorf("ObjectPath = %q", got)
	}
}

func TestEnvironmentBytesSplitAtTheFirstEqualsSign(t *testing.T) {
	got := EnvFromBytes([][]byte{[]byte("A=1=2"), []byte("NOEQUALS"), []byte("B=")})
	if want := []string{"A=1=2", "B="}; !reflect.DeepEqual(got, want) {
		t.Errorf("EnvFromBytes = %q; want %q", got, want)
	}
}

// connect is a connection to a fake bus answering with handler.
func connect(t *testing.T, handler dbustest.Handler) (*dbustest.Bus, *dbus.Conn) {
	t.Helper()
	bus := dbustest.Start(t, handler)
	c, err := dbus.Connect(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return bus, c
}

func TestTheUserManagersEnvironmentIsRead(t *testing.T) {
	bus, c := connect(t, func(call dbus.Message) dbustest.Answer {
		return dbustest.Return(dbus.Variant{Value: dbus.Array{Elem: "s", Items: []dbus.Value{
			dbus.Str("PATH=/usr/bin"), dbus.Str("NOEQUALS"), dbus.Str("A=1=2"),
		}}})
	})
	env, err := Environment(c)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"PATH=/usr/bin", "A=1=2"}; !reflect.DeepEqual(env, want) {
		t.Errorf("Environment = %q; want %q", env, want)
	}
	get := bus.WaitFor(t, "Get")
	if get.Destination != systemd || get.Path != systemdPath || get.Interface != "org.freedesktop.DBus.Properties" {
		t.Errorf("Get went to %q %q %q", get.Destination, get.Path, get.Interface)
	}
	if want := []dbus.Value{dbus.Str(manager), dbus.Str("Environment")}; !reflect.DeepEqual(get.Body, want) {
		t.Errorf("Get body = %#v", get.Body)
	}
}

func TestAnEnvironmentThatIsNoListOfStringsIsAnError(t *testing.T) {
	for name, body := range map[string][]dbus.Value{
		"no variant": {dbus.Str("x")},
		"no array":   {dbus.Variant{Value: dbus.Str("x")}},
		"nothing":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, c := connect(t, func(dbus.Message) dbustest.Answer { return dbustest.Return(body...) })
			_, err := Environment(c)
			var remote *dbus.RemoteError
			if !errors.As(err, &remote) || err.Error() != "logis.UnexpectedReply: Environment is not a list of strings" {
				t.Errorf("Environment = %v", err)
			}
		})
	}
}

func TestAScopeHoldsTheProcessInTheAppSlice(t *testing.T) {
	bus, c := connect(t, func(dbus.Message) dbustest.Answer {
		return dbustest.Return(dbus.ObjectPath("/org/freedesktop/systemd1/job/7"))
	})
	if err := StartScope(c, "app-logis-x-1.scope", 4242, "Anoint"); err != nil {
		t.Fatal(err)
	}
	call := bus.WaitFor(t, "StartTransientUnit")
	if call.Destination != systemd || call.Path != systemdPath || call.Interface != manager {
		t.Errorf("StartTransientUnit went to %q %q %q", call.Destination, call.Path, call.Interface)
	}
	prop := func(name string, v dbus.Value) dbus.Value {
		return dbus.Struct{dbus.Str(name), dbus.Variant{Value: v}}
	}
	want := []dbus.Value{
		dbus.Str("app-logis-x-1.scope"),
		dbus.Str("fail"),
		dbus.Array{Elem: "(sv)", Items: []dbus.Value{
			prop("PIDs", dbus.Array{Elem: "u", Items: []dbus.Value{dbus.U32(4242)}}),
			prop("Slice", dbus.Str("app.slice")),
			prop("Description", dbus.Str("Anoint")),
			prop("CollectMode", dbus.Str("inactive-or-failed")),
		}},
		dbus.Array{Elem: "(sa(sv))"},
	}
	if !reflect.DeepEqual(call.Body, want) {
		t.Errorf("body = %#v\nwant %#v", call.Body, want)
	}
}

func TestARefusedScopeIsAnError(t *testing.T) {
	_, c := connect(t, func(dbus.Message) dbustest.Answer {
		return dbustest.Fail("org.freedesktop.systemd1.UnitExists", "taken")
	})
	if err := StartScope(c, "u.scope", 1, "d"); err == nil || err.Error() != "org.freedesktop.systemd1.UnitExists: taken" {
		t.Errorf("StartScope = %v", err)
	}
}

func TestActivatableNamesRunOrCanBeStarted(t *testing.T) {
	_, c := connect(t, func(call dbus.Message) dbustest.Answer {
		switch call.Member {
		case "NameHasOwner":
			return dbustest.Return(dbus.Bool(call.Body[0] == dbus.Str("org.forge.Running")))
		case "ListActivatableNames":
			return dbustest.Return(dbus.Array{Elem: "s", Items: []dbus.Value{dbus.Str("org.forge.Sleeping")}})
		}
		return dbustest.Fail("org.freedesktop.DBus.Error.UnknownMethod", "no")
	})
	for name, want := range map[string]bool{"org.forge.Running": true, "org.forge.Sleeping": true, "org.forge.Absent": false} {
		if got, err := Activatable(c, name); got != want || err != nil {
			t.Errorf("Activatable(%s) = %v, %v; want %v", name, got, err, want)
		}
	}
}

func TestActivatableAsksTheBusDaemon(t *testing.T) {
	bus, c := connect(t, func(dbus.Message) dbustest.Answer { return dbustest.Return(dbus.Bool(true)) })
	if ok, err := Activatable(c, "org.forge.Bus"); !ok || err != nil {
		t.Fatalf("Activatable = %v, %v", ok, err)
	}
	call := bus.WaitFor(t, "NameHasOwner")
	if call.Destination != "org.freedesktop.DBus" || call.Path != "/org/freedesktop/DBus" || call.Interface != "org.freedesktop.DBus" {
		t.Errorf("NameHasOwner went to %q %q %q", call.Destination, call.Path, call.Interface)
	}
	if want := []dbus.Value{dbus.Str("org.forge.Bus")}; !reflect.DeepEqual(call.Body, want) {
		t.Errorf("body = %#v", call.Body)
	}
}

func TestActivationCarriesTheTokenAsPlatformData(t *testing.T) {
	bus, c := connect(t, func(dbus.Message) dbustest.Answer { return dbustest.Return() })
	if err := Activate(c, "org.forge.holy-oil", "", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := Activate(c, "org.forge.holy-oil", "wake", ""); err != nil {
		t.Fatal(err)
	}
	data := dbus.Array{Elem: "{sv}", Items: []dbus.Value{
		dbus.Entry{Key: dbus.Str("activation-token"), Value: dbus.Variant{Value: dbus.Str("tok")}},
		dbus.Entry{Key: dbus.Str("desktop-startup-id"), Value: dbus.Variant{Value: dbus.Str("tok")}},
	}}
	calls := bus.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d; want 2", len(calls))
	}
	activate, action := calls[0], calls[1]
	if activate.Member != "Activate" || activate.Destination != "org.forge.holy-oil" ||
		activate.Path != "/org/forge/holy_oil" || activate.Interface != "org.freedesktop.Application" {
		t.Errorf("Activate = %#v", activate)
	}
	if want := []dbus.Value{data}; !reflect.DeepEqual(activate.Body, want) {
		t.Errorf("Activate body = %#v", activate.Body)
	}
	want := []dbus.Value{dbus.Str("wake"), dbus.Array{Elem: "v"}, dbus.Array{Elem: "{sv}"}}
	if action.Member != "ActivateAction" || !reflect.DeepEqual(action.Body, want) {
		t.Errorf("ActivateAction = %#v", action)
	}
}
