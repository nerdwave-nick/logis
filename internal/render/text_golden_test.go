package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The text goldens (tests/goldens/text/{corpus,faces}@<scale>.png) against
// the text pass: the corpus lines on a panel, plus an ellipsised and a
// clipped column whose line features (clipping, the ellipsis) belong to the
// shaping slice; and a scene of theme faces.

var goldenSize = scale.Size{W: 330, H: 184}

var corpusInk = render.Hex(0xe8d8b0)

func corpus(t testing.TB) []string {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fonts", "corpus.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// corpusScene is the corpus lines on a panel, then the first line in a
// 150 px column twice, ellipsised and clipped.
func corpusScene(lines []string) *render.Scene {
	sc := &render.Scene{Clear: render.Hex(0x140c0a)}
	for _, y := range []float32{126, 150} {
		sc.Shapes = append(sc.Shapes, render.RoundedRect{Rect: [4]float32{8, y, 150, 21}, Radius: 3, Fill: render.Solid(render.Hex(0x3a2a20))})
	}
	for i, l := range lines {
		sc.Texts = append(sc.Texts, render.NewText(l, [2]float32{8, 6 + 24*float32(i)}, 16, corpusInk))
	}
	for i, o := range []render.Overflow{render.Ellipsis, render.Clip} {
		y := 6 + 24*float32(len(lines)+i)
		sc.Texts = append(sc.Texts, render.NewText(lines[0], [2]float32{8, y}, 16, corpusInk).InColumn(150, o))
	}
	return sc
}

// The whole corpus scene — Latin, accents, CJK and Hangul, Hebrew points
// and Arabic joining in a bidi line, emoji with skin tone and ZWJ
// sequences, an ellipsised and a clipped column — at 1.0, 1.3 and 2.0.
// LOGIS_UPDATE_GOLDENS=1 rewrites the goldens.
func TestTheCorpusMatchesItsGoldens(t *testing.T) {
	h := rendertest.Headless(t)
	h.SetFonts(pinnedFonts(t))
	rendertest.CheckScene(t, h, "text", "corpus", corpusScene(corpus(t)), goldenSize, goldenScales()...)
}

// The faces scene: the tracked label in a theme face, the row with colour
// spans and emphasis in another face, and the row with the emphasis
// underlined.
func TestTheFacesSceneMatchesItsGoldens(t *testing.T) {
	h := rendertest.Headless(t)
	h.SetFonts(pinnedFonts(t))
	if err := h.SetFaces(forgeFaces); err != nil {
		t.Fatal(err)
	}
	rendertest.CheckScene(t, h, "text", "faces", facesScene(), scale.Size{W: 220, H: 84}, goldenScales()...)
}

func goldenScales() []scale.Scale {
	return []scale.Scale{scale.From120ths(120), scale.From120ths(156), scale.From120ths(240)}
}
