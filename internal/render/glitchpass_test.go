package render

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/egl"
)

// The glitch pass takes one named uniform per control of
// effects.GlitchControls, the regions' and the moment's: each is active in
// the compiled program, so none is silently dropped. It runs before the
// CRT pass, on the picture before it is bent.
func TestEveryControlIsANamedUniformOfTheGlitchPass(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := egl.NewSurfaceless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatal(err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	defer ctx.Close()
	p, err := newGlitchPass()
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	for i, k := range effects.GlitchControls {
		if p.controls[i] < 0 {
			t.Errorf("%s.%s: no uniform %s", k.Part, k.Key, k.Uniform)
		}
	}
	for i := range MaxRegions {
		if p.region[i] < 0 || p.regionRadius[i] < 0 {
			t.Errorf("region %d has no uniform", i)
		}
	}
	if p.count < 0 || p.event < 0 || p.time < 0 {
		t.Error("the region count, the burst or the time")
	}
	if !strings.HasPrefix(glitchFragment, "#version 300 es\n// Original implementation for logis from a behavioural specification (clean\n// room). MIT, the logis project.") {
		t.Error("the clean room's header")
	}
}

func TestTheGlitchPassRunsBeforeTheCRTPass(t *testing.T) {
	h, err := OpenHeadless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatal(err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	defer h.Close()
	if len(h.passes) != 2 {
		t.Fatalf("%d passes", len(h.passes))
	}
	if _, ok := h.passes[0].(*glitchPass); !ok {
		t.Errorf("first %T", h.passes[0])
	}
	if _, ok := h.passes[1].(*crtPass); !ok {
		t.Errorf("then %T", h.passes[1])
	}
}
