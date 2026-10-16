package launch_test

// Launching applications for real: the fixture application
// tests/fixtures/apps/bin/anoint records how it was started; a fake session
// bus (dbustest) stands in for systemd's user manager and for D-Bus
// activation. Nothing here reaches the real session bus.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/dbus"
	"github.com/nerdwave-nick/logis/internal/dbus/dbustest"
	"github.com/nerdwave-nick/logis/internal/launch"
)

func bin(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "apps", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// A rite is where a test's launch records itself, and the file that holds
// it alive.
type rite struct {
	t   *testing.T
	dir string
}

func newRite(t *testing.T) *rite {
	r := &rite{t, t.TempDir()}
	if err := os.WriteFile(r.hold(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Whatever happens, the application ends with the test.
	t.Cleanup(func() { _ = os.Remove(r.hold()) })
	return r
}

func (r *rite) record() string { return filepath.Join(r.dir, "record") }
func (r *rite) hold() string   { return filepath.Join(r.dir, "hold") }

// env is the environment of a launch, marker telling its source apart.
func (r *rite) env(marker string) []string {
	return []string{
		"PATH=" + bin(r.t) + ":/usr/bin:/bin",
		"HOME=" + r.dir,
		"ANOINT_RECORD=" + r.record(),
		"ANOINT_HOLD=" + r.hold(),
		"ANOINT_MARKER=" + marker,
		"XDG_ACTIVATION_TOKEN=stale-token",
	}
}

// heard is what the application recorded: each key's values in order.
func (r *rite) heard() map[string][]string {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(r.record()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the application never ran")
		}
		time.Sleep(5 * time.Millisecond)
	}
	text, err := os.ReadFile(r.record())
	if err != nil {
		r.t.Fatal(err)
	}
	heard := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(text), "\n"), "\n") {
		key, value, _ := strings.Cut(line, " ")
		heard[key] = append(heard[key], value)
	}
	return heard
}

// release lets the application end, and waits until it is gone (reaped).
func (r *rite) release(pid int) {
	r.t.Helper()
	_ = os.Remove(r.hold())
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the application lingers (a zombie?)")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func one(t *testing.T, heard map[string][]string, key string) string {
	t.Helper()
	if len(heard[key]) == 0 {
		t.Fatalf("the application recorded no %s", key)
	}
	return heard[key][0]
}

func anoint(args ...string) launch.Plan {
	return launch.Plan{AppID: "forge-anoint", Name: "Anoint", Argv: append([]string{"anoint"}, args...)}
}

// environ is the environment process pid started with.
func environ(t *testing.T, pid int) []string {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

func TestWithoutABusTheAppRunsDetachedInTheCallersEnvironment(t *testing.T) {
	t.Setenv("LOGIS_DAEMON_ONLY", "stale")
	r := newRite(t)
	session := launch.Session{Bus: "unix:path=/nonexistent/logis-test-bus", Env: r.env("caller")}
	launched, err := launch.Launch(anoint("--vessel", "two words"), "tok-1", session)
	if err != nil {
		t.Fatal(err)
	}
	if launched.Via != launch.Plain {
		t.Errorf("via = %v; want plain", launched.Via)
	}
	if len(launched.Warnings) != 1 || !strings.HasPrefix(launched.Warnings[0], "no session bus: the session bus is unreachable: ") {
		t.Errorf("warnings = %q", launched.Warnings)
	}
	pid := launched.PID
	heard := r.heard()
	if got := one(t, heard, "pid"); got != strconv.Itoa(pid) {
		t.Errorf("pid = %s; want %d", got, pid)
	}
	if want := []string{"--vessel", "two words"}; !reflect.DeepEqual(heard["arg"], want) {
		t.Errorf("args = %q; want %q", heard["arg"], want)
	}
	if got := one(t, heard, "marker"); got != "caller" {
		t.Errorf("marker = %q; want the caller's", got)
	}
	if got := one(t, heard, "token"); got != "tok-1" {
		t.Errorf("token = %q; want the new token, not the stale one", got)
	}
	if got := one(t, heard, "startup"); got != "tok-1" {
		t.Errorf("startup id = %q", got)
	}
	if got := one(t, heard, "cwd"); got != r.dir {
		t.Errorf("cwd = %q; want the home dir", got)
	}
	if got := one(t, heard, "sid"); got != strconv.Itoa(pid) {
		t.Errorf("sid = %s; want a session of its own (%d)", got, pid)
	}
	for _, kv := range environ(t, pid) {
		if strings.HasPrefix(kv, "LOGIS_DAEMON_ONLY=") {
			t.Errorf("the daemon's own environment leaked: %s", kv)
		}
		if kv == "XDG_ACTIVATION_TOKEN=stale-token" {
			t.Errorf("the stale token survived beside the new one")
		}
	}
	r.release(pid)
}

func TestWithoutATokenNoStaleOneIsPassedOn(t *testing.T) {
	r := newRite(t)
	plan := anoint()
	plan.Cwd = bin(t)
	launched, err := launch.Launch(plan, "", launch.Session{Env: r.env("caller")})
	if err != nil {
		t.Fatal(err)
	}
	if launched.Via != launch.Plain || len(launched.Warnings) != 0 {
		t.Errorf("via %v, warnings %q; want plain and none", launched.Via, launched.Warnings)
	}
	heard := r.heard()
	if got := one(t, heard, "token"); got != "" {
		t.Errorf("token = %q; want none", got)
	}
	if got := one(t, heard, "startup"); got != "" {
		t.Errorf("startup id = %q; want none", got)
	}
	if got := one(t, heard, "cwd"); got != bin(t) {
		t.Errorf("cwd = %q; want the entry's Path", got)
	}
	r.release(launched.PID)
}

// systemd is a user manager that knows env and accepts transient units.
func systemd(t *testing.T, env []string) *dbustest.Bus {
	items := make([]dbus.Value, len(env))
	for i, kv := range env {
		items[i] = dbus.Str(kv)
	}
	return dbustest.Start(t, func(call dbus.Message) dbustest.Answer {
		switch call.Member {
		case "Get":
			return dbustest.Return(dbus.Variant{Value: dbus.Array{Elem: "s", Items: items}})
		case "StartTransientUnit":
			return dbustest.Return(dbus.ObjectPath("/org/freedesktop/systemd1/job/7"))
		}
		return dbustest.Fail("org.freedesktop.DBus.Error.UnknownMethod", "no")
	})
}

func property(t *testing.T, properties []dbus.Value, name string) dbus.Value {
	t.Helper()
	for _, p := range properties {
		if f, ok := p.(dbus.Struct); ok && len(f) == 2 && f[0] == dbus.Str(name) {
			if v, ok := f[1].(dbus.Variant); ok {
				return v.Value
			}
		}
	}
	t.Fatalf("no property %s", name)
	return nil
}

func TestWithAUserManagerTheAppGetsTheSessionEnvironmentAndAScope(t *testing.T) {
	r := newRite(t)
	bus := systemd(t, r.env("session"))
	launched, err := launch.Launch(anoint(), "tok-2", launch.Session{Bus: bus.Address, Env: r.env("caller")})
	if err != nil {
		t.Fatal(err)
	}
	pid := launched.PID
	heard := r.heard()
	if got := one(t, heard, "marker"); got != "session" {
		t.Errorf("marker = %q; systemd's environment wins", got)
	}
	if got := one(t, heard, "token"); got != "tok-2" {
		t.Errorf("token = %q", got)
	}

	get := bus.WaitFor(t, "Get")
	if want := []dbus.Value{dbus.Str("org.freedesktop.systemd1.Manager"), dbus.Str("Environment")}; !reflect.DeepEqual(get.Body, want) {
		t.Errorf("Get body = %#v", get.Body)
	}
	unit := bus.WaitFor(t, "StartTransientUnit")
	if unit.Destination != "org.freedesktop.systemd1" {
		t.Errorf("destination = %q", unit.Destination)
	}
	if len(unit.Body) != 4 {
		t.Fatalf("body = %#v", unit.Body)
	}
	name, _ := unit.Body[0].(dbus.Str)
	mode, _ := unit.Body[1].(dbus.Str)
	properties, _ := unit.Body[2].(dbus.Array)
	if !strings.HasPrefix(string(name), `app-logis-forge\x2danoint-`) || !strings.HasSuffix(string(name), ".scope") {
		t.Errorf("unit name = %q", name)
	}
	if launched.Via != launch.Scope || launched.Unit != string(name) {
		t.Errorf("via %v %q; want the scope %q", launched.Via, launched.Unit, name)
	}
	if mode != "fail" {
		t.Errorf("mode = %q", mode)
	}
	if got, want := property(t, properties.Items, "PIDs"), (dbus.Array{Elem: "u", Items: []dbus.Value{dbus.U32(pid)}}); !reflect.DeepEqual(got, want) {
		t.Errorf("PIDs = %#v; the process itself goes into the scope", got)
	}
	if got := property(t, properties.Items, "Slice"); got != dbus.Str("app.slice") {
		t.Errorf("Slice = %#v", got)
	}
	if got := property(t, properties.Items, "Description"); got != dbus.Str("Anoint") {
		t.Errorf("Description = %#v", got)
	}
	// The fake user manager never moves the process.
	if want := []string{string(name) + " did not take the process in time"}; !reflect.DeepEqual(launched.Warnings, want) {
		t.Errorf("warnings = %q; want %q", launched.Warnings, want)
	}
	r.release(pid)
}

func TestAProgramNotOnThePathIsNotFoundAndNothingStarts(t *testing.T) {
	r := newRite(t)
	for _, program := range []string{"forge-no-such-program", "", "bin/anoint", "/nonexistent/anoint"} {
		plan := anoint()
		plan.Argv = []string{program}
		if program == "" {
			plan.Argv = nil
		}
		_, err := launch.Launch(plan, "", launch.Session{Env: r.env("caller")})
		var nf *launch.NotFoundError
		if !errors.As(err, &nf) || nf.Program != program {
			t.Errorf("Launch(%q) = %v; want not found", program, err)
		}
	}
	_, err := launch.Launch(launch.Plan{Argv: []string{"forge-no-such-program"}}, "", launch.Session{})
	if err == nil || err.Error() != "no program 'forge-no-such-program' answers the summons" {
		t.Errorf("error text = %v", err)
	}
	if _, statErr := os.Stat(r.record()); statErr == nil {
		t.Error("something started")
	}
}

func TestAProgramOutsideThePathRunsByItsAbsolutePath(t *testing.T) {
	r := newRite(t)
	plan := anoint()
	plan.Argv = []string{filepath.Join(bin(t), "anoint"), "--abs"}
	env := r.env("caller")
	env[0] = "PATH=/usr/bin:/bin"
	launched, err := launch.Launch(plan, "", launch.Session{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.heard()["arg"]; !reflect.DeepEqual(got, []string{"--abs"}) {
		t.Errorf("args = %q", got)
	}
	r.release(launched.PID)
}

// activation is a bus on which the activatable names can be activated.
func activation(t *testing.T, activatable ...string) *dbustest.Bus {
	names := make([]dbus.Value, len(activatable))
	for i, n := range activatable {
		names[i] = dbus.Str(n)
	}
	return dbustest.Start(t, func(call dbus.Message) dbustest.Answer {
		switch call.Member {
		case "ListActivatableNames":
			return dbustest.Return(dbus.Array{Elem: "s", Items: names})
		case "NameHasOwner":
			return dbustest.Return(dbus.Bool(false))
		case "Activate", "ActivateAction":
			return dbustest.Return()
		}
		return dbustest.Fail("org.freedesktop.DBus.Error.UnknownMethod", "no")
	})
}

func tokenData(token string) dbus.Value {
	entry := func(k string) dbus.Value {
		return dbus.Entry{Key: dbus.Str(k), Value: dbus.Variant{Value: dbus.Str(token)}}
	}
	return dbus.Array{Elem: "{sv}", Items: []dbus.Value{entry("activation-token"), entry("desktop-startup-id")}}
}

func TestADBusActivatableAppIsActivatedWithTheToken(t *testing.T) {
	bus := activation(t, "org.forge.Bus")
	session := launch.Session{Bus: bus.Address}
	plan := launch.Plan{AppID: "org.forge.Bus", Name: "Bus", DBus: true}
	launched, err := launch.Launch(plan, "tok-3", session)
	if err != nil {
		t.Fatal(err)
	}
	if launched.Via != launch.Bus || launched.PID != 0 {
		t.Errorf("via %v, pid %d; want the bus and no process", launched.Via, launched.PID)
	}
	call := bus.WaitFor(t, "Activate")
	if call.Destination != "org.forge.Bus" || call.Path != "/org/forge/Bus" || call.Interface != "org.freedesktop.Application" {
		t.Errorf("Activate went to %q %q %q", call.Destination, call.Path, call.Interface)
	}
	if want := []dbus.Value{tokenData("tok-3")}; !reflect.DeepEqual(call.Body, want) {
		t.Errorf("Activate body = %#v", call.Body)
	}

	plan.Action = "wake"
	if _, err := launch.Launch(plan, "tok-4", session); err != nil {
		t.Fatal(err)
	}
	call = bus.WaitFor(t, "ActivateAction")
	if want := []dbus.Value{dbus.Str("wake"), dbus.Array{Elem: "v"}, tokenData("tok-4")}; !reflect.DeepEqual(call.Body, want) {
		t.Errorf("ActivateAction body = %#v", call.Body)
	}
}

func TestADBusAppTheBusCannotStartRunsItsExecOrFails(t *testing.T) {
	r := newRite(t)
	bus := activation(t)
	session := launch.Session{Bus: bus.Address, Env: r.env("caller")}
	plan := anoint("--fallback")
	plan.DBus = true
	launched, err := launch.Launch(plan, "", session)
	if err != nil {
		t.Fatal(err)
	}
	if launched.Via != launch.Plain {
		t.Errorf("via = %v; this bus has no user manager either", launched.Via)
	}
	if len(launched.Warnings) == 0 || launched.Warnings[0] != "the bus cannot activate forge-anoint; its Exec runs instead" {
		t.Errorf("warnings = %q; the refused activation and scope are noted", launched.Warnings)
	}
	if got := r.heard()["arg"]; !reflect.DeepEqual(got, []string{"--fallback"}) {
		t.Errorf("args = %q", got)
	}
	r.release(launched.PID)

	plan.Argv = nil
	_, err = launch.Launch(plan, "", session)
	var na *launch.NotActivatableError
	if !errors.As(err, &na) || err.Error() != "the bus cannot wake 'forge-anoint' and it names no Exec" {
		t.Errorf("Launch(only D-Bus) = %v", err)
	}
}

func TestAProcessThatCannotStartIsASpawnError(t *testing.T) {
	r := newRite(t)
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	// Executable, but no interpreter the kernel can run.
	if err := os.WriteFile(broken, []byte("#!/nonexistent/interpreter\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan := anoint()
	plan.Argv = []string{broken}
	_, err := launch.Launch(plan, "", launch.Session{Env: r.env("caller")})
	var se *launch.SpawnError
	if !errors.As(err, &se) || !strings.HasPrefix(err.Error(), "the process would not start: ") {
		t.Errorf("Launch = %v; want a spawn error", err)
	}
}

func TestHowAnAppStartedReadsAsAWord(t *testing.T) {
	for via, want := range map[launch.Via]string{launch.Plain: "plain", launch.Scope: "scope", launch.Bus: "bus", launch.Via(9): "Via(9)"} {
		if got := fmt.Sprint(via); got != want {
			t.Errorf("Via %d reads %q; want %q", int(via), got, want)
		}
	}
}
