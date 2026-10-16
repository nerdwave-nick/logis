package daemon

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// A daemon left running across an upgrade (feat/logis-go 750bed66; owner
// decision 2026-10-16): it keeps serving, but once its binary on disk was
// replaced since it started (an upgrade renames a new file over it, or a
// build rewrites it), every client it greets hears a reminder to restart
// it, in its welcome (the client prints it on stderr), as a notice over
// each session it shows (a keybind's logis has no stderr to read), and
// once in its log. It restarts nothing itself: a session may be open, and
// under systemd the unit is the owner's to restart. A daemon of another
// protocol version refuses plainly instead (core.go, protocol.Version).

// selfExe is the running binary, as the kernel links it: stat follows the
// link to the running image even once its file is gone.
const selfExe = "/proc/self/exe"

// deleted is what the kernel appends to /proc/self/exe's target once the
// file was unlinked (or another renamed over it).
const deleted = " (deleted)"

// exeFile identifies a binary on disk: its file, and what an in-place
// rewrite changes.
type exeFile struct {
	dev, ino uint64
	size     int64
	mtime    int64
}

// exeWatch tells whether the daemon's binary was replaced since it started.
type exeWatch struct {
	// path is where the running binary was started from; running is its
	// image; known: both could be told.
	path    string
	running exeFile
	known   bool
	stat    func(path string) (exeFile, error)
}

// newExeWatch watches the binary that link (readlink) and stat tell for
// /proc/self/exe now. One that cannot be told is never replaced.
func newExeWatch(link func(string) (string, error), stat func(string) (exeFile, error)) *exeWatch {
	w := &exeWatch{stat: stat}
	target, err := link(selfExe)
	if err != nil || target == "" {
		return w
	}
	running, err := stat(selfExe)
	if err != nil {
		return w
	}
	w.path, w.running, w.known = strings.TrimSuffix(target, deleted), running, true
	return w
}

// watchExe watches this process's binary.
func watchExe() *exeWatch { return newExeWatch(os.Readlink, statExe) }

// statExe tells the file at path, following links.
func statExe(path string) (exeFile, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return exeFile{}, err
	}
	return exeFile{dev: st.Dev, ino: st.Ino, size: st.Size, mtime: st.Mtim.Nano()}, nil
}

// replaced: another file now stands where the binary was started from (an
// uninstalled one, with nothing there, is not "newer").
func (w *exeWatch) replaced() bool {
	if w == nil || !w.known {
		return false
	}
	now, err := w.stat(w.path)
	return err == nil && now != w.running
}

// reminder asks for this daemon's restart: through systemd when it passed
// the socket (the socket starts the new binary on the next connection),
// else by ending this process (the next client starts the new one).
func reminder(activated bool, pid int) string {
	how := "kill " + strconv.Itoa(pid)
	if activated {
		how = "systemctl --user restart logis"
	}
	return "a newer logis is installed — restart the daemon (" + how + ")"
}

// staleness is the daemon's stale check: the reminder while its binary is
// replaced, else "".
func staleness(w *exeWatch, activated bool, pid int) func() string {
	return func() string {
		if !w.replaced() {
			return ""
		}
		return reminder(activated, pid)
	}
}

// welcome greets cn: with the reminder, said once in the log too, while
// the binary is replaced. Its sessions are toasted with it (reveal).
func (c *core) welcome(cn *conn) {
	cn.notice = c.stale()
	if cn.notice != "" && !c.staleLogged {
		c.staleLogged = true
		c.logf(cn.notice)
	}
	cn.phase = ready
	cn.peer.send(protocol.Welcome(c.pid, cn.notice))
}

// reveal shows s, and over it the reminder its client was greeted with.
func (c *core) reveal(s Session) {
	c.resolver.Show(s)
	cn, ok := c.conns[s.ID]
	if !ok || cn.notice == "" {
		return
	}
	if toaster, ok := c.resolver.(Toaster); ok {
		toaster.Toast(Toast{Kind: ToastStale, Text: cn.notice})
	}
}
