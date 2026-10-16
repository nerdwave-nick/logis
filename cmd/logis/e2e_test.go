package main

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
)

// End to end in a test compositor started by internal/e2e (a nested niri
// with a generated config and a scratch runtime dir locally; headless sway
// on CI). Runs only with LOGIS_E2E set; otherwise the tests skip. No input
// is injected: these tests only look, through the harness's verified
// connection (screencopy).

// nestedPatience bounds every wait on the panel.
const nestedPatience = e2e.Patience

// startCompositor starts the test compositor LOGIS_E2E asks for at the
// output scale, and the harness's connection to it.
func startCompositor(t *testing.T, scale string) (*e2e.Compositor, *e2e.Harness) {
	t.Helper()
	c := e2e.Start(t, e2e.Requested(t), e2e.Scale(scale))
	return c, e2e.Connect(t, c)
}

// requireNiri skips a test that needs nested niri's IPC (niri msg).
func requireNiri(t *testing.T, c *e2e.Compositor) {
	t.Helper()
	if c.Kind() != e2e.NestedNiri {
		t.Skipf("needs nested niri, not %s", c.Kind().Name())
	}
}

// screenshot captures the output upright; with LOGIS_E2E_SHOTS set, the
// capture is also kept there.
func screenshot(t *testing.T, h *e2e.Harness, name string) image.Image {
	t.Helper()
	img := h.Capture()
	if dir := os.Getenv("LOGIS_E2E_SHOTS"); dir != "" {
		path := filepath.Join(dir, t.Name()[strings.LastIndex(t.Name(), "/")+1:]+"-"+name+".png")
		_ = rendertest.WritePNG(path, img)
	}
	return &image.RGBA{Pix: img.Pixels, Stride: img.Width * 4, Rect: image.Rect(0, 0, img.Width, img.Height)}
}

// panelProcess is a running `logis daemon --debug-panel` and its stderr.
type panelProcess struct {
	t     *testing.T
	cmd   *exec.Cmd
	lines chan string
	all   strings.Builder
	exit  chan error
}

func startPanel(t *testing.T, c *e2e.Compositor, extraEnv ...string) *panelProcess {
	t.Helper()
	cmd := c.Command(buildLogis(t), "daemon", "--debug-panel")
	cmd.Env = append(cmd.Env, extraEnv...)
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
// fails with what is wrong if it never is. The reference draws its text
// with the fonts c gives its clients (the pinned test fonts).
func checkPanel(t *testing.T, c *e2e.Compositor, h *e2e.Harness, name string, f frame, scale float64) {
	t.Helper()
	t.Setenv("FONTCONFIG_FILE", filepath.Join(c.RuntimeDir(), "fonts.conf"))
	want, err := reference(f, scale)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for attempt, deadline := 1, time.Now().Add(nestedPatience); time.Now().Before(deadline); attempt++ {
		var summary string
		if summary, problems = panelProblems(screenshot(t, h, name), want); len(problems) == 0 {
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
	e2e.Requested(t)
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
			c, h := startCompositor(t, tc.scale)
			p := startPanel(t, c, tc.env...)
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
			checkPanel(t, c, h, "panel", f, scale)
		})
	}
}

func TestE2EAScaleChangeRedrawsAtTheNewScale(t *testing.T) {
	c, h := startCompositor(t, "1.0")
	requireNiri(t, c)
	p := startPanel(t, c)
	p.expect(glass)
	for _, scale := range []string{"2", "1.3", "1"} {
		c.Msg("output", "winit", "scale", scale)
		m := p.expect(glass)
		want, _ := strconv.ParseFloat(scale, 64)
		if got, _ := strconv.ParseFloat(m[5], 64); got != want {
			t.Fatalf("redrawn at scale %v after the output went to %v", got, want)
		}
		checkPanel(t, c, h, "scale-"+scale, parseGlass(m), want)
	}
}

func TestE2ELosingTheCompositorExits2(t *testing.T) {
	c, _ := startCompositor(t, "1.0")
	p := startPanel(t, c)
	p.expect(glass)
	c.Kill()
	p.expect(`^logis: the noosphere fell silent mid-rite: `)
	if code := p.exitCode(); code != 2 {
		t.Errorf("exit %d after the compositor died, want 2", code)
	}
}

func TestE2ESigtermEndsThePanelCleanly(t *testing.T) {
	c, _ := startCompositor(t, "1.0")
	p := startPanel(t, c)
	p.expect(glass)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if code := p.exitCode(); code != 0 {
		t.Errorf("exit %d after SIGTERM, want 0", code)
	}
}
