package dbus_test

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/dbus"
	"github.com/nerdwave-nick/logis/internal/dbus/dbustest"
)

func vars(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

func TestTheSessionAddressFallsBackToTheRuntimeDir(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"given", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/x", "XDG_RUNTIME_DIR": "/run/u"}, "unix:path=/x"},
		{"empty is unset", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "", "XDG_RUNTIME_DIR": "/run/u"}, "unix:path=/run/u/bus"},
		{"fallback", map[string]string{"XDG_RUNTIME_DIR": "/run/u"}, "unix:path=/run/u/bus"},
		{"relative runtime dir", map[string]string{"XDG_RUNTIME_DIR": "rel"}, ""},
		{"nothing", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dbus.SessionAddress(vars(tc.env)); got != tc.want {
				t.Errorf("SessionAddress = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestAddressesNameUnixSocketsWithEscapes(t *testing.T) {
	got := dbus.Sockets("tcp:host=x,port=1;unix:path=/run/a%20b/bus,guid=00ff;unix:abstract=/tmp/dbus-X;unix:path=/c%zz%4")
	want := []dbus.Socket{{Name: "/run/a b/bus"}, {Name: "/tmp/dbus-X", Abstract: true}, {Name: "/c%zz%4"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sockets = %#v; want %#v", got, want)
	}
	if got := dbus.Sockets("unix:tmpdir=/tmp"); len(got) != 0 {
		t.Errorf("Sockets(tmpdir) = %#v; want none", got)
	}
}

var ping = dbus.Call{Destination: "org.forge", Path: "/org/forge", Interface: "org.forge.Rite", Member: "Ping"}

func TestACallGetsItsReplyPastTheSignalsBeforeIt(t *testing.T) {
	bus := dbustest.Start(t, func(call dbus.Message) dbustest.Answer {
		if call.Member == "Ping" {
			return dbustest.Return(dbus.Str("pong"))
		}
		return dbustest.Fail("org.forge.Error.Heresy", "no such rite")
	})
	c, err := dbus.Connect(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	pong := []dbus.Value{dbus.Str("pong")}
	if got, err := c.Call(ping); err != nil || !reflect.DeepEqual(got, pong) {
		t.Fatalf("Ping = %#v, %v", got, err)
	}
	other := ping
	other.Member = "Other"
	_, err = c.Call(other)
	var remote *dbus.RemoteError
	if !errors.As(err, &remote) || remote.Name != "org.forge.Error.Heresy" || remote.Message != "no such rite" {
		t.Fatalf("Other = %v; want the remote error", err)
	}
	if err.Error() != "org.forge.Error.Heresy: no such rite" {
		t.Errorf("error text = %q", err)
	}
	if got, err := c.Call(ping); err != nil || !reflect.DeepEqual(got, pong) {
		t.Fatalf("still usable: Ping = %#v, %v", got, err)
	}
}

func TestAMissingBusIsAnError(t *testing.T) {
	_, err := dbus.Connect("unix:path=/nonexistent/logis-bus")
	var unreachable *dbus.UnreachableError
	if !errors.As(err, &unreachable) {
		t.Errorf("Connect(missing path) = %v; want unreachable", err)
	}
	if _, err := dbus.Connect("tcp:host=x"); !errors.Is(err, dbus.ErrNoAddress) {
		t.Errorf("Connect(tcp) = %v; want no address", err)
	}
	if err := dbus.ErrNoAddress.Error(); err != "no session bus address" {
		t.Errorf("ErrNoAddress = %q", err)
	}
}

func TestTheFirstReachableSocketIsUsed(t *testing.T) {
	bus := dbustest.Start(t, func(dbus.Message) dbustest.Answer { return dbustest.Return() })
	c, err := dbus.Connect("unix:path=/nonexistent/logis-bus;" + bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

// A bus at an abstract address is reached like one on a path.
func TestAnAbstractSocketIsReached(t *testing.T) {
	name := "logis-test-" + filepath.Base(t.TempDir())
	l, err := net.Listen("unix", "@"+name)
	if err != nil {
		t.Skipf("no abstract sockets here: %v", err)
	}
	defer func() { _ = l.Close() }()
	accepted := make(chan struct{})
	go func() {
		conn, err := l.Accept()
		if err == nil {
			close(accepted)
			_ = conn.Close()
		}
	}()
	// The listener hangs up before authenticating.
	if _, err := dbus.Connect("unix:abstract=" + name); err == nil {
		t.Fatal("Connect succeeded against a mute listener")
	}
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the abstract socket was never reached")
	}
}

// refuser is a bus that rejects every authentication.
func refuser(t *testing.T) string {
	dir, err := os.MkdirTemp("", "logis-refuser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "bus")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		if _, err := r.ReadString('\n'); err == nil {
			_, _ = conn.Write([]byte("REJECTED EXTERNAL\r\n"))
		}
		_, _ = r.ReadString('\n')
	}()
	return "unix:path=" + socket
}

func TestARefusedAuthenticationIsAnError(t *testing.T) {
	_, err := dbus.Connect(refuser(t))
	var auth *dbus.AuthError
	if !errors.As(err, &auth) || err.Error() != "the session bus refused us: REJECTED EXTERNAL" {
		t.Fatalf("Connect = %v; want the refusal", err)
	}
}

func TestAnUnresponsiveBusTimesOut(t *testing.T) {
	t.Parallel()
	bus := dbustest.Start(t, func(dbus.Message) dbustest.Answer { return dbustest.Silent() })
	c, err := dbus.Connect(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_, err = c.Call(ping)
	var unreachable *dbus.UnreachableError
	if !errors.As(err, &unreachable) || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Call = %v; want a timeout", err)
	}
}

func TestSendDoesNotWait(t *testing.T) {
	bus := dbustest.Start(t, func(dbus.Message) dbustest.Answer { return dbustest.Silent() })
	c, err := dbus.Connect(bus.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	first, err := c.Send(ping)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Send(ping)
	if err != nil || second != first+1 {
		t.Fatalf("serials %d then %d (%v); want consecutive", first, second, err)
	}
	if got := bus.WaitFor(t, "Ping"); got.Interface != "org.forge.Rite" {
		t.Errorf("the bus heard %#v", got)
	}
}
