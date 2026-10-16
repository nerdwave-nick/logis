package render

import (
	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// pass is one post-processing pass: it draws a finished frame, a texture of
// the target's size with GL's bottom-up rows, into a target with its
// effects. The renderer runs the passes a scene's Post wants in their
// order, each reading what the one before drew, the last drawing into the
// target; a pass placed before the CRT pass works on the picture before it
// is bent.
type pass interface {
	wanted(post Post) bool
	draw(frame gles.Texture, t Target, post Post)
	close()
}

// frames are the intermediate frames the passes read: the scene's, then
// each pass's but the last, alternating between two. They are kept while
// the target's size stays; nothing here waits on the GPU.
type frames struct{ t [2]*gles.Target }

// get is frame i (0 or 1) at size, made or remade to it.
func (f *frames) get(i int, size scale.Size) *gles.Target {
	if t := f.t[i]; t == nil || t.W != int(size.W) || t.H != int(size.H) {
		if t != nil {
			t.Delete()
		}
		f.t[i] = gles.NewTarget(int(size.W), int(size.H))
	}
	return f.t[i]
}

func (f *frames) close() {
	for i, t := range f.t {
		if t != nil {
			t.Delete()
			f.t[i] = nil
		}
	}
}

// drawPassed draws sc into an intermediate frame, then runs the passes
// sc.Post wants (n of them, at least one) into t.
func (r *Renderer) drawPassed(t Target, sc *Scene, n int) {
	at := func(f *gles.Target) Target { return Target{FBO: f.FBO, Size: t.Size, Scale: t.Scale} }
	src := r.frames.get(0, t.Size)
	r.drawScene(at(src), sc)
	done := 0
	for _, p := range r.passes {
		if !p.wanted(sc.Post) {
			continue
		}
		done++
		if done == n {
			p.draw(gles.Texture(src.Tex), t, sc.Post)
			return
		}
		dst := r.frames.get(done%2, t.Size)
		p.draw(gles.Texture(src.Tex), at(dst), sc.Post)
		src = dst
	}
}

// wanted is how many of the passes sc's Post wants.
func (r *Renderer) wanted(post Post) int {
	n := 0
	for _, p := range r.passes {
		if p.wanted(post) {
			n++
		}
	}
	return n
}
