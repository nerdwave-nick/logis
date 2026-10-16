package render

import (
	"errors"
	"os"
	"testing"
)

func headless(t *testing.T) *Headless {
	t.Helper()
	h, err := OpenHeadless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatalf("LOGIS_REQUIRE_EGL is set, but: %v", err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	t.Cleanup(h.Close)
	return h
}

func TestAFullAtlasAtItsLargestSideRefusesAnotherImageAndKeepsItsOwn(t *testing.T) {
	a := headless(t).Atlas()
	a.maxSide = 1024 // as if the context took no larger texture
	img := &Image{Width: 300, Height: 300, Pixels: make([]byte, 300*300*4)}
	var err error
	n := 0
	for ; n < 20 && err == nil; n++ {
		_, err = a.Insert(img)
	}
	var full *AtlasFullError
	if !errors.As(err, &full) || full.Side != 1024 {
		t.Fatalf("after %d images: %v, want the 1024 atlas full", n, err)
	}
	if err.Error() != "the 1024x1024 image atlas is full; no reliquary remains for another image" {
		t.Errorf("message %q", err)
	}
	// Three shelves of three: nine 301-pixel cells in 1024.
	if a.Len() != 9 || a.Side() != 1024 {
		t.Errorf("%d images at side %d, want 9 at 1024", a.Len(), a.Side())
	}
}
