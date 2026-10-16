package window

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// A visible panel asks for another size (a theme of another size worn
// while it shows): the request goes out with the next frame's commit, and
// the compositor's configure, which may leave the size to the client,
// brings frames at the new size.
func TestAVisiblePanelAsksForAnotherSize(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	p := &painter{}
	showConfigured(t, f, w, p)
	f.frameDone()
	w.Resize(size(500, 300))
	until(t, w, f, "size 500 300", "commit")
	f.frameDone()
	f.configure(7, 0, 0)
	until(t, w, f, "ack 7", "destination 500 300", "commit")
	want := Geometry{Logical: size(500, 300), Physical: size(500, 300), Scale: scale.One}
	if last := p.frames[len(p.frames)-1]; last != want {
		t.Errorf("drawn at %+v, want %+v", last, want)
	}
	w.Hide()
	w.Resize(size(400, 200)) // nothing shows: nothing to ask
}
