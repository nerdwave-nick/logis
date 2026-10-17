package cogitator

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/window"
)

// A toast shows its whole text: the window wraps it with its measured
// widths, and a long one stays until it is read.

func TestTheWindowWrapsAToastWithItsMeasuredWidths(t *testing.T) {
	r := newRig()
	// Twenty pixels a character: twice the font's average advance, so
	// lines only fit when measured.
	r.ui.measure = func(window.Geometry) scene.Measure {
		return scene.MeasureFunc(func(x render.Text) (float32, bool) { return 20 * float32(utf8.RuneCountInString(x.Text)), true })
	}
	r.show(1, list.DefaultOptions(), "a", "b", "c")
	long := strings.TrimSpace(strings.Repeat("the config is heretical ", 4))
	r.ui.toast(daemon.Toast{Kind: daemon.ToastConfigRejected, Error: long})
	sc := r.paintOnce(t)
	words := "the config is heretical: " + long
	var lines []string
	for _, x := range sc.Texts {
		if x.Column && strings.Contains(words, x.Text) && len(x.Text) > 1 {
			if width := 20 * float32(len(x.Text)); width > x.Width {
				t.Errorf("%q is %v wide in a line of %v", x.Text, width, x.Width)
			}
			lines = append(lines, x.Text)
		}
	}
	if len(lines) < 2 || strings.Join(lines, " ") != words {
		t.Errorf("lines %q", lines)
	}
}

func TestALongToastFromTheDaemonStaysUntilItIsRead(t *testing.T) {
	r := newRig()
	now := time.Unix(1000, 0)
	r.ui.now = func() time.Time { return now }
	r.show(1, list.DefaultOptions(), "a")
	long := strings.Repeat("why ", 40)
	r.ui.toast(daemon.Toast{Kind: daemon.ToastConfigRejected, Error: long})
	n := list.Notice{Text: "the config is heretical: " + long}
	until, ok := r.ui.deadline()
	if !ok || until != now.Add(n.ReadFor(noticeFor)) || until == now.Add(noticeFor) {
		t.Fatalf("until %v (%v)", until.Sub(now), ok)
	}
}
