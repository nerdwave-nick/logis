package main

import (
	"bufio"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// End to end in a nested niri the test starts itself, with a generated
// config (never the owner's) and a scratch runtime dir, as Rust's
// tests/support/e2e does. Runs only with LOGIS_E2E=niri (it opens a window
// in the running session, so WAYLAND_DISPLAY must name one); otherwise the
// tests skip. No input is injected anywhere: these tests only look.

const niriConfig = `// logis test compositor (nested niri); generated, never the owner's config.
output "winit" {
    scale %s
}
hotkey-overlay {
    skip-at-startup
}
animations {
    off
}
xwayland-satellite {
    off
}
layout {
    background-color "#404040"
}
prefer-no-csd
`

// nestedPatience bounds every wait on the nested niri and the panel in it.
const nestedPatience = 10 * time.Second

type nestedNiri struct {
	t      *testing.T
	dir    string
	cmd    *exec.Cmd
	socket string // the nested WAYLAND_DISPLAY, a name in dir
}

func requireNiri(t *testing.T) {
	t.Helper()
	if os.Getenv("LOGIS_E2E") != "niri" {
		t.Skip("SKIPPED: set LOGIS_E2E=niri (nested niri in the running session)")
	}
}

// parentSocket is the running session's socket, absolute: the nested
// niri's own socket goes to the scratch runtime dir.
func parentSocket(t *testing.T) string {
	t.Helper()
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		t.Fatal("nested niri opens a window, so it needs WAYLAND_DISPLAY of a running session")
	}
	if filepath.IsAbs(display) {
		return display
	}
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), display)
}

func startNiri(t *testing.T, scale string) *nestedNiri {
	t.Helper()
	dir, err := os.MkdirTemp("", "logis-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "niri.kdl")
	if err := os.WriteFile(config, fmt.Appendf(nil, niriConfig, scale), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "niri.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("niri", "-c", config)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"XDG_RUNTIME_DIR=" + dir, "WAYLAND_DISPLAY=" + parentSocket(t), "RUST_LOG=niri=warn",
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start nested niri: %v", err)
	}
	n := &nestedNiri{t: t, dir: dir, cmd: cmd}
	t.Cleanup(func() {
		n.stop()
		_ = log.Close()
		_ = os.RemoveAll(dir)
	})
	deadline := time.Now().Add(nestedPatience)
	for n.socket == "" {
		if time.Now().After(deadline) {
			t.Fatalf("nested niri opened no socket:\n%s", n.log())
		}
		if names, _ := filepath.Glob(filepath.Join(dir, "wayland-[0-9]*")); len(names) > 0 {
			for _, name := range names {
				if !strings.HasSuffix(name, ".lock") {
					n.socket = filepath.Base(name)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

func (n *nestedNiri) log() string {
	b, _ := os.ReadFile(filepath.Join(n.dir, "niri.log"))
	return string(b)
}

func (n *nestedNiri) stop() {
	if n.cmd.ProcessState == nil {
		_ = n.cmd.Process.Signal(syscall.SIGTERM)
		_ = n.cmd.Wait()
	}
}

func (n *nestedNiri) env(extra ...string) []string {
	return append([]string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"XDG_RUNTIME_DIR=" + n.dir, "WAYLAND_DISPLAY=" + n.socket,
	}, extra...)
}

// msg runs `niri msg` against the nested niri (its IPC socket).
func (n *nestedNiri) msg(args ...string) {
	n.t.Helper()
	sockets, _ := filepath.Glob(filepath.Join(n.dir, "niri.*.sock"))
	if len(sockets) == 0 {
		n.t.Fatal("the nested niri has no IPC socket")
	}
	cmd := exec.Command("niri", append([]string{"msg"}, args...)...)
	cmd.Env = n.env("NIRI_SOCKET=" + sockets[0])
	if out, err := cmd.CombinedOutput(); err != nil {
		n.t.Fatalf("niri msg %v: %v\n%s", args, err, out)
	}
}

// screenshot captures the nested output with grim.
func (n *nestedNiri) screenshot(name string) image.Image {
	n.t.Helper()
	path := filepath.Join(n.dir, name+".png")
	cmd := exec.Command("grim", path)
	cmd.Env = n.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		n.t.Fatalf("grim: %v\n%s", err, out)
	}
	f, err := os.Open(path)
	if err != nil {
		n.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		n.t.Fatal(err)
	}
	if dir := os.Getenv("LOGIS_E2E_SHOTS"); dir != "" {
		_ = exec.Command("cp", path, filepath.Join(dir, n.t.Name()[strings.LastIndex(n.t.Name(), "/")+1:]+"-"+name+".png")).Run()
	}
	return img
}

// panelProcess is a running `logis daemon --debug-panel` and its stderr.
type panelProcess struct {
	t     *testing.T
	cmd   *exec.Cmd
	lines chan string
	all   strings.Builder
	exit  chan error
}

func startPanel(t *testing.T, n *nestedNiri, extraEnv ...string) *panelProcess {
	t.Helper()
	cmd := exec.Command(buildLogis(t), "daemon", "--debug-panel")
	cmd.Env = n.env(extraEnv...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &panelProcess{t: t, cmd: cmd, lines: make(chan string, 64), exit: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
		p.exit <- cmd.Wait()
	}()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.exit:
			case <-time.After(nestedPatience):
				_ = cmd.Process.Kill()
			}
		}
	})
	return p
}

// expect waits for a stderr line matching re and returns its submatches.
func (p *panelProcess) expect(re string) []string {
	p.t.Helper()
	pattern := regexp.MustCompile(re)
	timeout := time.After(nestedPatience)
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				status := "still running"
				select {
				case err := <-p.exit:
					status = fmt.Sprintf("%v", err)
					p.exit <- err
				case <-time.After(nestedPatience):
				}
				p.t.Fatalf("logis ended (%s) without %q; stderr:\n%s", status, re, p.all.String())
			}
			p.all.WriteString(line + "\n")
			if m := pattern.FindStringSubmatch(line); m != nil {
				return m
			}
		case <-timeout:
			p.t.Fatalf("no %q in %v; stderr:\n%s", re, nestedPatience, p.all.String())
		}
	}
}

// exitCode waits for logis to end.
func (p *panelProcess) exitCode() int {
	p.t.Helper()
	for range p.lines {
	}
	select {
	case <-p.exit:
	case <-time.After(nestedPatience):
		p.t.Fatal("logis did not exit")
	}
	return p.cmd.ProcessState.ExitCode()
}

var glass = `cogitator glass (\d+)x(\d+) px for (\d+)x(\d+) logical at scale ([\d.]+)`

// frame is what one glass line reported.
type frame struct{ physW, physH, logW, logH int }

func parseGlass(m []string) frame {
	atoi := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return frame{atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])}
}

// checkPanel captures the output until the panel drawn for f is on it
// (the compositor shows a committed frame with its next repaint), and
// fails with what is wrong if it never is.
func checkPanel(t *testing.T, n *nestedNiri, name string, f frame, scale float64) {
	t.Helper()
	want, err := reference(f, scale)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for attempt, deadline := 1, time.Now().Add(nestedPatience); time.Now().Before(deadline); attempt++ {
		var summary string
		if summary, problems = panelProblems(n.screenshot(name), want); len(problems) == 0 {
			t.Logf("%s after %d captures", summary, attempt)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestE2EDebugPanelIsDrawnAtEachOutputScale(t *testing.T) {
	requireNiri(t)
	for _, tc := range []struct {
		scale string
		env   []string
	}{
		{"1.0", nil},
		{"1.3", nil},
		{"2.0", nil},
		{"1.3", []string{"LIBGL_ALWAYS_SOFTWARE=1"}},
	} {
		name := tc.scale
		if tc.env != nil {
			name += "-llvmpipe"
		}
		t.Run(name, func(t *testing.T) {
			n := startNiri(t, tc.scale)
			p := startPanel(t, n, tc.env...)
			spirit := p.expect(`the machine spirit wakes on (.*)`)[1]
			if tc.env != nil && !strings.Contains(spirit, "llvmpipe") {
				t.Errorf("LIBGL_ALWAYS_SOFTWARE=1 woke %q, not llvmpipe", spirit)
			}
			m := p.expect(glass)
			scale, _ := strconv.ParseFloat(tc.scale, 64)
			if got, _ := strconv.ParseFloat(m[5], 64); got != scale {
				t.Errorf("drawn at scale %v, output scale %v", got, scale)
			}
			f := parseGlass(m)
			if want := int(float64(f.logW)*scale + 0.5); f.physW != want {
				t.Errorf("buffer %d px wide for %d logical at %v, want %d", f.physW, f.logW, scale, want)
			}
			checkPanel(t, n, "panel", f, scale)
		})
	}
}

func TestE2EAScaleChangeRedrawsAtTheNewScale(t *testing.T) {
	requireNiri(t)
	n := startNiri(t, "1.0")
	p := startPanel(t, n)
	p.expect(glass)
	for _, scale := range []string{"2", "1.3", "1"} {
		n.msg("output", "winit", "scale", scale)
		m := p.expect(glass)
		want, _ := strconv.ParseFloat(scale, 64)
		if got, _ := strconv.ParseFloat(m[5], 64); got != want {
			t.Fatalf("redrawn at scale %v after the output went to %v", got, want)
		}
		checkPanel(t, n, "scale-"+scale, parseGlass(m), want)
	}
}

func TestE2ELosingTheCompositorExits2(t *testing.T) {
	requireNiri(t)
	n := startNiri(t, "1.0")
	p := startPanel(t, n)
	p.expect(glass)
	_ = n.cmd.Process.Kill()
	_ = n.cmd.Wait()
	p.expect(`^logis: the noosphere fell silent mid-rite: `)
	if code := p.exitCode(); code != 2 {
		t.Errorf("exit %d after the compositor died, want 2", code)
	}
}

func TestE2ESigtermEndsThePanelCleanly(t *testing.T) {
	requireNiri(t)
	n := startNiri(t, "1.0")
	p := startPanel(t, n)
	p.expect(glass)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if code := p.exitCode(); code != 0 {
		t.Errorf("exit %d after SIGTERM, want 0", code)
	}
}
