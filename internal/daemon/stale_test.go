package daemon

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// disk is a fake file system for the exe watch: what each path is now.
type disk map[string]exeFile

func (d disk) stat(path string) (exeFile, error) {
	if f, ok := d[path]; ok {
		return f, nil
	}
	return exeFile{}, fs.ErrNotExist
}

// linked is /proc/self/exe's link as the kernel tells it.
func linked(target string) func(string) (string, error) {
	return func(string) (string, error) { return target, nil }
}

var installed = exeFile{dev: 1, ino: 100, size: 9_000_000, mtime: 1_700_000_000_000_000_000}

func TestTheDaemonsBinaryIsReplacedOnlyWhenAnotherFileTakesItsPlace(t *testing.T) {
	for _, c := range []struct {
		name string
		link string
		now  disk // the path's file now; the running image is selfExe
		want bool
	}{
		{"untouched", "/usr/bin/logis", disk{"/usr/bin/logis": installed}, false},
		{"upgraded: a new file renamed over it", "/usr/bin/logis (deleted)",
			disk{"/usr/bin/logis": {dev: 1, ino: 101, size: 9_000_000, mtime: installed.mtime}}, true},
		{"rewritten in place: same inode, new time", "/usr/bin/logis",
			disk{"/usr/bin/logis": {dev: 1, ino: 100, size: 9_000_000, mtime: installed.mtime + 1}}, true},
		{"rewritten in place: same inode, new size", "/usr/bin/logis",
			disk{"/usr/bin/logis": {dev: 1, ino: 100, size: 9_100_000, mtime: installed.mtime}}, true},
		{"uninstalled: nothing newer is there", "/usr/bin/logis (deleted)", disk{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			now := disk{selfExe: installed}
			for path, f := range c.now {
				now[path] = f
			}
			// What the daemon started from is what /proc/self/exe is.
			w := newExeWatch(linked(c.link), disk{selfExe: installed}.stat)
			w.stat = now.stat
			if got := w.replaced(); got != c.want {
				t.Errorf("replaced %v, want %v", got, c.want)
			}
		})
	}
}

func TestADaemonThatCannotTellItsBinaryNeverClaimsAReplacement(t *testing.T) {
	broken := func(string) (string, error) { return "", errors.New("no /proc") }
	if w := newExeWatch(broken, disk{selfExe: installed}.stat); w.replaced() {
		t.Error("replaced without a path")
	}
	if w := newExeWatch(linked("/usr/bin/logis"), disk{}.stat); w.replaced() {
		t.Error("replaced without its own image")
	}
	var none *exeWatch
	if none.replaced() {
		t.Error("a nil watch replaced")
	}
}

// The real stat: the test binary's own watch, and a file renamed over a path.
func TestTheRealStatSeesAFileRenamedOverThePath(t *testing.T) {
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary's own watch: it is not replaced while it runs.
	if w := watchExe(); w.replaced() {
		t.Fatalf("the test binary %s counts as replaced", self)
	}
	// The same check on a file this test owns, through the real stat.
	path := filepath.Join(dir, "logis")
	if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := newExeWatch(linked(path), func(string) (exeFile, error) { return statExe(path) })
	w.stat = statExe
	if w.replaced() {
		t.Fatal("replaced before the upgrade")
	}
	if err := os.WriteFile(path+".new", []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if !w.replaced() {
		t.Fatal("not replaced after the upgrade")
	}
}

func TestTheReminderSaysHowToRestartThisDaemon(t *testing.T) {
	if got := reminder(true, 4242); got != "logis updated — systemctl --user restart logis" {
		t.Errorf("socket-activated: %q", got)
	}
	if got := reminder(false, 4242); got != "logis updated — restart the daemon: kill 4242" {
		t.Errorf("started by a client: %q", got)
	}
}

// staleCore is a test core whose binary was replaced (or not).
func staleCore(replaced *bool) (*core, *recorder, *[]string) {
	c, r := newTestCore()
	var logged []string
	c.logf = func(m string) { logged = append(logged, m) }
	c.stale = func() string {
		if *replaced {
			return "logis updated — restart the daemon: kill 4242"
		}
		return ""
	}
	return c, r, &logged
}

func TestAReplacedDaemonWelcomesWithTheReminderAndLogsItOnce(t *testing.T) {
	replaced := false
	c, _, logged := staleCore(&replaced)
	greet(t, c, 1) // not yet: a plain welcome
	replaced = true
	const want = "logis updated — restart the daemon: kill 4242"
	for id := SessionID(2); id <= 3; id++ {
		w := &wire{}
		c.connect(id, w)
		c.receive(id, protocol.Hello(), nil)
		if got := w.take(); !reflect.DeepEqual(got, []protocol.Reply{protocol.Welcome(pid, want)}) {
			t.Fatalf("greeting %d: %+v", id, got)
		}
	}
	if !reflect.DeepEqual(*logged, []string{want}) {
		t.Errorf("logged %q", *logged)
	}
}

func TestAReplacedDaemonToastsTheReminderOverEachSessionItShowsAndStillServesIt(t *testing.T) {
	replaced := true
	c, r, _ := staleCore(&replaced)
	w := &wire{}
	c.connect(1, w)
	c.receive(1, protocol.Hello(), nil)
	c.receive(1, protocol.Open(protocol.ModeDmenu, nil), nil)
	c.receive(1, itemsOf("oil", "ignition"), nil)
	c.receive(1, protocol.End(), nil)
	log := r.take()
	if len(log) < 2 || !strings.HasPrefix(log[0], "show 1 dmenu") ||
		log[1] != "toast stale logis updated — restart the daemon: kill 4242" {
		t.Fatalf("%q", log)
	}
	c.answered(1, Answer{Kind: AnswerItem, Index: 1})
	replies := w.take()
	last := replies[len(replies)-1]
	if last.Type != protocol.TypeResult || last.Outcome.Kind != protocol.Selected || last.Outcome.Index != 1 {
		t.Fatalf("%+v", replies)
	}
	r.take()
	// An app session's launcher is toasted the same way.
	c.connect(2, &wire{})
	c.receive(2, protocol.Hello(), nil)
	c.receive(2, protocol.Open(protocol.ModeApps, nil), nil)
	if log := r.take(); len(log) < 2 || log[1] != "toast stale logis updated — restart the daemon: kill 4242" {
		t.Fatalf("%q", log)
	}
}

func TestAnUnreplacedDaemonToastsNothing(t *testing.T) {
	replaced := false
	c, r, logged := staleCore(&replaced)
	open(t, c, 1, protocol.ModeApps)
	for _, l := range r.take() {
		if strings.HasPrefix(l, "toast") {
			t.Fatalf("toasted %q", l)
		}
	}
	if len(*logged) != 0 {
		t.Fatalf("logged %q", *logged)
	}
}

// A client of another protocol version (4: the version before open's
// rites) is refused plainly: restart the daemon.
func TestAClientOfTheVersionBeforeIsRefusedWithARestart(t *testing.T) {
	c, _ := newTestCore()
	w := &wire{}
	c.connect(1, w)
	c.receive(1, protocol.Request{Type: protocol.TypeHello, Version: 4}, nil)
	replies := w.take()
	if !isError(replies, "the client speaks protocol 4, this machine spirit protocol 5") ||
		!strings.Contains(replies[0].Message, "restart the daemon") || !w.closed {
		t.Errorf("replies %+v", replies)
	}
}
