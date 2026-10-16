package e2e

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Tests of the harness's pure parts and its isolation guard: these
// always run.

func TestTheInjectorRefusesASocketServedByAnyOtherProcess(t *testing.T) {
	// A socket this process serves: its peer is us, not a compositor the
	// harness spawned, exactly like the live session's socket would be.
	path := filepath.Join(t.TempDir(), "wayland-0")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	fd := dial(t, path)
	defer func() { _ = syscall.Close(fd) }()
	spawned := exec.Command("true")
	if err := spawned.Run(); err != nil {
		t.Fatal(err)
	}
	err = VerifyPeer(fd, spawned.Process.Pid)
	if err == nil || !strings.Contains(err.Error(), "refusing to inject") {
		t.Errorf("a foreign server was accepted: %v", err)
	}
	if err := VerifyPeer(fd, os.Getpid()); err != nil {
		t.Errorf("its own server was refused: %v", err)
	}
	if _, err := ConnectVerified(path, spawned.Process.Pid); err == nil || !strings.Contains(err.Error(), "refusing to inject") {
		t.Errorf("ConnectVerified accepted a foreign server: %v", err)
	}
}

func dial(t *testing.T, path string) int {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestTheCompositorChoiceIsNiriSwayOrNone(t *testing.T) {
	for value, want := range map[string]Kind{"": None, " ": None, "niri": NestedNiri, "sway": HeadlessSway} {
		if got, err := ParseKind(value); err != nil || got != want {
			t.Errorf("ParseKind(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	if _, err := ParseKind("gnome"); err == nil {
		t.Error("gnome was accepted")
	}
	if NestedNiri.Name() != "niri-nested" || HeadlessSway.Name() != "sway-headless" {
		t.Errorf("names %q, %q", NestedNiri.Name(), HeadlessSway.Name())
	}
}

func TestTheUSLayoutTypesShiftedCharactersWithShift(t *testing.T) {
	for c, want := range map[rune]struct {
		code  uint32
		shift bool
	}{'a': {30, false}, 'A': {30, true}, '?': {53, true}, ' ': {KeySpace, false}, '\n': {KeyEnter, false}} {
		code, shift, ok := USKey(c)
		if !ok || code != want.code || shift != want.shift {
			t.Errorf("USKey(%q) = %d, %v, %v", c, code, shift, ok)
		}
	}
	if _, _, ok := USKey('é'); ok {
		t.Error("é is on the US layout")
	}
}

func TestCapturesConvertFromBothByteOrdersAndFlipWhenInverted(t *testing.T) {
	// Two rows of one pixel, stride 4: red on top, blue below (XRGB: B G R X).
	xrgb := []byte{0, 0, 255, 0, 255, 0, 0, 0}
	check := func(img *render.Image, err error, want []byte) {
		t.Helper()
		if err != nil || !bytes.Equal(img.Pixels, want) {
			t.Errorf("pixels %v, %v; want %v", img, err, want)
		}
	}
	img, err := ToRGBA(xrgb, ShmXRGB8888, 1, 2, 4, false)
	check(img, err, []byte{255, 0, 0, 255, 0, 0, 255, 255})
	img, err = ToRGBA(xrgb, ShmXRGB8888, 1, 2, 4, true)
	check(img, err, []byte{0, 0, 255, 255, 255, 0, 0, 255})
	xbgr := []byte{255, 0, 0, 0, 0, 0, 255, 0}
	img, err = ToRGBA(xbgr, ShmXBGR8888, 1, 2, 4, false)
	check(img, err, []byte{255, 0, 0, 255, 0, 0, 255, 255})
	if _, err := ToRGBA(xrgb, 0x36314752, 1, 2, 4, false); err == nil {
		t.Error("an RGB565 capture was read")
	}
}

func TestOutputTransformsTurnCapturesUpright(t *testing.T) {
	// 2x2: a b / c d
	img := &render.Image{Width: 2, Height: 2}
	for v := byte(1); v <= 4; v++ {
		img.Pixels = append(img.Pixels, v, v, v, 255)
	}
	order := func(transform int32) []byte {
		up, err := Upright(img, transform)
		if err != nil {
			t.Fatal(err)
		}
		var out []byte
		for i := 0; i < len(up.Pixels); i += 4 {
			out = append(out, up.Pixels[i])
		}
		return out
	}
	for transform, want := range map[int32][]byte{
		TransformNormal: {1, 2, 3, 4}, TransformFlipped180: {3, 4, 1, 2},
		TransformFlipped: {2, 1, 4, 3}, Transform180: {4, 3, 2, 1},
	} {
		if got := order(transform); !bytes.Equal(got, want) {
			t.Errorf("transform %d: %v, want %v", transform, got, want)
		}
	}
	if _, err := Upright(img, 1); err == nil {
		t.Error("a rotated output was turned")
	}
}

func TestACentredCropSettlesAnOddRemainderByTheReference(t *testing.T) {
	// A 5x3 capture with a 2x1 marker at x 2..4, y 1: centring rounds down
	// to x 1, the reference finds x 2.
	capture := &render.Image{Width: 5, Height: 3, Pixels: bytes.Repeat([]byte{0, 0, 0, 255}, 15)}
	for x := 2; x < 4; x++ {
		copy(capture.Pixels[(5+x)*4:], []byte{255, 255, 255, 255})
	}
	reference := &render.Image{Width: 2, Height: 1, Pixels: bytes.Repeat([]byte{255, 255, 255, 255}, 2)}
	if got := CropCentred(capture, 2, 1, reference); !bytes.Equal(got.Pixels, reference.Pixels) {
		t.Errorf("with the reference: %v", got.Pixels)
	}
	if got := CropCentred(capture, 2, 1, nil); bytes.Equal(got.Pixels, reference.Pixels) {
		t.Error("without the reference the crop is not at the centre")
	}
}

func TestAPanelMapsSurfacePointsThroughAMirroredAxis(t *testing.T) {
	// The compositor mirrors y (nested niri's flipped output).
	p := Calibrate([2][2]float64{{100, 100}, {150, 140}}, [2][2]float64{{10, 50}, {60, 10}})
	if p.Sign != [2]float64{1, -1} {
		t.Fatalf("signs %v", p.Sign)
	}
	if x, y := p.At(30, 20); x != 120 || y != 130 {
		t.Errorf("surface (30, 20) at output (%v, %v), want (120, 130)", x, y)
	}
}

// WaitUntil returns once a condition over all events so far holds, which
// no single event satisfies; the process need not be a compositor's.
func TestWaitUntilWaitsForAStateTheEventsBuildUp(t *testing.T) {
	p := Spawn(t, exec.Command("sh", "-c",
		`for i in 1 2 3; do echo "logis-trace: $i tick" >&2; sleep 0.05; done; exec sleep 10`))
	ticks := func(events []trace.Event) int {
		n := 0
		for _, e := range events {
			if e.What == "tick" {
				n++
			}
		}
		return n
	}
	p.WaitUntil("three ticks", func(events []trace.Event) bool { return ticks(events) == 3 })
	if n := ticks(p.Events()); n != 3 {
		t.Fatalf("%d ticks", n)
	}
}

// A capture must wait for the frame that draws the latest change: a commit
// stamped after it, not the one before it, and not input or frame feedback
// that came after the change.
func TestWaitForDrawnWaitsForACommitAfterTheLatestChange(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	script := `printf '%s\n' 'logis-trace: 10 show 1' 'logis-trace: 11 commit' 'logis-trace: 20 rites items 3' \
		'logis-trace: 21 key 28 released' 'logis-trace: 22 presented' >&2
while [ ! -e "$1" ]; do sleep 0.01; done
printf '%s\n' 'logis-trace: 30 commit' >&2
sleep 5`
	p := Spawn(t, exec.Command("sh", "-c", script, "sh", gate))
	p.WaitSeen("the feedback after the change", Is("presented"))
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if e := p.WaitForDrawn(); e.NS != 30 {
		t.Errorf("WaitForDrawn returned %+v, want the commit at 30", e)
	}
}

func TestOnlyReportsOfAChangeToTheWindowAreChanges(t *testing.T) {
	for what, want := range map[string]bool{
		"show 3": true, "stage 3": true, "items 12": true, "update 0": true, "remove 1": true,
		`toast "the theme is heretical"`: true, "untoast": true, `query "o"`: true, "select 2": true,
		"select none": true, "tab rites": true, "theme forge-crimson-brass": true, "rites items 4": true,
		"commit": false, "commit closing": false, "settled": false, "frame-done": false, "presented": false,
		"key 28 pressed": false, "modifiers 1 0 0 0": false, "keyboard-enter": false, "accept 0": false,
		"hide 3": false, "blur 0 0 10 10": false, "token 3": false, "items": true, "itemsx": false,
	} {
		if got := changesTheWindow(what); got != want {
			t.Errorf("changesTheWindow(%q) = %v, want %v", what, got, want)
		}
	}
}
