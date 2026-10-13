package svg

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"
	"testing"
)

// render draws the document text into a w×h image at its own size.
func render(t *testing.T, text string, w, h int) (*image.RGBA, []string) {
	t.Helper()
	doc, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	report := doc.Render(dst, Identity)
	return dst, report
}

// doc wraps body in an svg root of w×h user units.
func doc(w, h int, body string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="%d">%s</svg>`, w, h, body)
}

func px(img *image.RGBA, x, y int) [4]uint8 {
	o := img.PixOffset(x, y)
	return [4]uint8(img.Pix[o : o+4])
}

// near: every channel within tol.
func near(a, b [4]uint8, tol uint8) bool {
	for i := range 4 {
		if max(a[i], b[i])-min(a[i], b[i]) > tol {
			return false
		}
	}
	return true
}

func wantPx(t *testing.T, img *image.RGBA, x, y int, want [4]uint8, tol uint8) {
	t.Helper()
	if got := px(img, x, y); !near(got, want, tol) {
		t.Errorf("pixel (%d, %d) = %v, want %v ±%d", x, y, got, want, tol)
	}
}

// coverage is the summed alpha over the image, in whole pixels.
func coverage(img *image.RGBA) float64 {
	var sum float64
	for i := 3; i < len(img.Pix); i += 4 {
		sum += float64(img.Pix[i]) / 255
	}
	return sum
}

func wantCoverage(t *testing.T, img *image.RGBA, want, tol float64) {
	t.Helper()
	if got := coverage(img); math.Abs(got-want) > tol {
		t.Errorf("coverage %.2f px², want %.2f ±%.2f", got, want, tol)
	}
}

var (
	red   = [4]uint8{255, 0, 0, 255}
	clear = [4]uint8{}
	black = [4]uint8{0, 0, 0, 255}
)

func TestARectFillsExactlyItsPixels(t *testing.T) {
	img, report := render(t, doc(10, 10, `<rect x="2" y="3" width="4" height="5" fill="red"/>`), 10, 10)
	for y := range 10 {
		for x := range 10 {
			want := clear
			if x >= 2 && x < 6 && y >= 3 && y < 8 {
				want = red
			}
			wantPx(t, img, x, y, want, 0)
		}
	}
	if len(report) != 0 {
		t.Errorf("report %q, want nothing unsupported", report)
	}
}

func TestEdgesAreAntiAliasedByCoverage(t *testing.T) {
	img, _ := render(t, doc(4, 4, `<rect x="0.5" y="0" width="2" height="4"/>`), 4, 4)
	wantPx(t, img, 0, 1, [4]uint8{0, 0, 0, 128}, 1)
	wantPx(t, img, 1, 1, black, 0)
	wantPx(t, img, 2, 1, [4]uint8{0, 0, 0, 128}, 1)
	wantPx(t, img, 3, 1, clear, 0)
}

func TestTheDefaultFillIsBlackAndBasicShapesHaveTheirArea(t *testing.T) {
	for _, tc := range []struct {
		body string
		area float64
	}{
		{`<circle cx="20" cy="20" r="10"/>`, math.Pi * 100},
		{`<ellipse cx="20" cy="20" rx="15" ry="5"/>`, math.Pi * 75},
		{`<rect x="5" y="5" width="30" height="20" rx="5"/>`, 600 - (4-math.Pi)*25},
		// rx follows ry, then each is clamped to half the side.
		{`<rect x="5" y="5" width="30" height="20" ry="50"/>`, 600 - (4-math.Pi)*150},
		{`<polygon points="0,0 40,0 0,40"/>`, 800},
		{`<polyline points="0 0, 40 0, 40 40"/>`, 800},
		{`<path d="M0 0 H40 V40 Z"/>`, 800},
		{`<path d="m0 0 h40 v40 z m0 0"/>`, 800},
		{`<path d="M0,0 L40,0 L0,40"/>`, 800},
	} {
		t.Run(tc.body, func(t *testing.T) {
			img, _ := render(t, doc(40, 40, tc.body), 40, 40)
			wantCoverage(t, img, tc.area, 1.5)
		})
	}
}

func TestCurvesAndArcsFollowThePathGrammar(t *testing.T) {
	for _, tc := range []struct {
		body string
		area float64
	}{
		// Half discs of radius 10.
		{`<path d="M0 20 A10 10 0 0 1 20 20 Z"/>`, math.Pi * 50},
		{`<path d="M0,20a10,10,0,0,0,20,0z"/>`, math.Pi * 50},
		// A radius too small is scaled up to reach the end point.
		{`<path d="M0 20 A1 1 0 0 1 20 20 Z"/>`, math.Pi * 50},
		// Quarter disc from flags packed without separators.
		{`<path d="M20 20 L20 0 A20 20 0 0140 20 Z"/>`, math.Pi * 100},
		// A cubic and its smooth reflection make a symmetric lens area; the
		// quadratic T reflection too.
		{`<path d="M0 20 C0 0 40 0 40 20 S0 40 0 20Z"/>`, 0},
		{`<path d="M0 20 Q20 0 40 20 T0 20 Z"/>`, 0},
	} {
		t.Run(tc.body, func(t *testing.T) {
			img, _ := render(t, doc(40, 40, tc.body), 40, 40)
			if tc.area == 0 {
				if c := coverage(img); c < 100 {
					t.Errorf("coverage %.1f, want a filled curve", c)
				}
				return
			}
			wantCoverage(t, img, tc.area, 1.5)
		})
	}
}

func TestTransformsApplyToGroupsAndShapes(t *testing.T) {
	for _, tc := range []struct {
		body   string
		inside [][2]int
	}{
		{`<g transform="translate(10 5)"><rect width="2" height="2" fill="red"/></g>`, [][2]int{{10, 5}, {11, 6}}},
		{`<rect width="2" height="2" fill="red" transform="scale(3)"/>`, [][2]int{{0, 0}, {5, 5}}},
		{`<rect width="2" height="2" fill="red" transform="scale(2, 1) translate(5,0)"/>`, [][2]int{{10, 0}, {13, 1}}},
		{`<rect x="9" y="0" width="2" height="4" fill="red" transform="rotate(90 10 10)"/>`, [][2]int{{17, 9}, {19, 10}}},
		{`<rect width="2" height="2" fill="red" transform="matrix(1 0 0 1 4 4)"/>`, [][2]int{{4, 4}, {5, 5}}},
		{`<rect width="2" height="10" fill="red" transform="skewX(45)"/>`, [][2]int{{10, 9}, {2, 1}}},
	} {
		t.Run(tc.body, func(t *testing.T) {
			img, _ := render(t, doc(20, 20, tc.body), 20, 20)
			for _, p := range tc.inside {
				if a := px(img, p[0], p[1])[3]; a < 200 {
					t.Errorf("pixel %v alpha %d, want inside", p, a)
				}
			}
			if px(img, 0, 19)[3] != 0 {
				t.Error("pixel (0, 19) is painted")
			}
		})
	}
}

func TestTheRootViewBoxMapsOntoTheSize(t *testing.T) {
	text := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10" viewBox="0 0 4 4"><rect width="4" height="4" fill="red"/></svg>`
	d, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := d.Size(); w != 20 || h != 10 {
		t.Errorf("size %vx%v, want 20x10", w, h)
	}
	img, _ := render(t, text, 20, 10)
	// xMidYMid meet: a 10×10 square centred.
	wantPx(t, img, 4, 5, clear, 0)
	wantPx(t, img, 5, 5, red, 0)
	wantPx(t, img, 14, 5, red, 0)
	wantPx(t, img, 15, 5, clear, 0)
}

func TestTheSizeComesFromTheViewBoxUnitsOrADefault(t *testing.T) {
	for _, tc := range []struct {
		root string
		w, h float64
	}{
		{`viewBox="0 0 48 24"`, 48, 24},
		{`viewBox="0,0,48,24" width="100%" height="50%"`, 48, 12},
		{`width="1in" height="12pt"`, 96, 16},
		{`width="64px" height="2em"`, 64, 32},
		{``, 100, 100},
	} {
		d, err := Parse([]byte(`<svg xmlns="http://www.w3.org/2000/svg" ` + tc.root + `/>`))
		if err != nil {
			t.Fatalf("%s: %v", tc.root, err)
		}
		if w, h := d.Size(); math.Abs(w-tc.w) > 1e-9 || math.Abs(h-tc.h) > 1e-9 {
			t.Errorf("%s: size %vx%v, want %vx%v", tc.root, w, h, tc.w, tc.h)
		}
	}
}

func TestFillRulesDecideHoles(t *testing.T) {
	// Two squares, both clockwise: nonzero fills the inner one, evenodd
	// cuts it out.
	shape := `<path d="M0 0H30V30H0Z M10 10H20V20H10Z" fill-rule="%s"/>`
	img, _ := render(t, doc(30, 30, fmt.Sprintf(shape, "nonzero")), 30, 30)
	wantPx(t, img, 15, 15, black, 0)
	img, _ = render(t, doc(30, 30, fmt.Sprintf(shape, "evenodd")), 30, 30)
	wantPx(t, img, 15, 15, clear, 0)
	wantPx(t, img, 5, 5, black, 0)
	wantCoverage(t, img, 800, 1)
	// Counter-wound inner square: a hole under both rules.
	img, _ = render(t, doc(30, 30, `<path d="M0 0H30V30H0Z M10 10V20H20V10Z"/>`), 30, 30)
	wantPx(t, img, 15, 15, clear, 0)
}

func TestColoursParseInEveryNotation(t *testing.T) {
	for _, tc := range []struct {
		fill string
		want [4]uint8
	}{
		{`fill="#f00"`, red},
		{`fill="#FF0000"`, red},
		{`fill="rgb(255, 0, 0)"`, red},
		{`fill="rgb(100%,0%,0%)"`, red},
		{`fill="RED"`, red},
		{`fill="rebeccapurple"`, [4]uint8{0x66, 0x33, 0x99, 255}},
		{`fill="#00ff0080"`, [4]uint8{0, 128, 0, 128}},
		{`fill="rgba(0,0,255,0.5)"`, [4]uint8{0, 0, 128, 128}},
		{`fill="currentColor" color="red"`, red},
		{`fill="none"`, clear},
		{`fill="transparent"`, clear},
		{`fill="bogus"`, black},
		{`fill="url(#missing) red"`, red},
		{`fill="url(#missing)"`, clear},
	} {
		img, _ := render(t, doc(4, 4, `<rect width="4" height="4" `+tc.fill+`/>`), 4, 4)
		if got := px(img, 1, 1); !near(got, tc.want, 1) {
			t.Errorf("%s: %v, want %v", tc.fill, got, tc.want)
		}
	}
}

func TestPropertiesCascadeFromAttributesStylesheetsAndStyle(t *testing.T) {
	body := `<style>.a { fill: #00f } #b { fill: lime; } rect.c, .d { fill: red }</style>
		<g fill="red"><rect width="2" height="2"/></g>
		<rect class="a" x="2" width="2" height="2" fill="red"/>
		<rect id="b" class="a" x="4" width="2" height="2"/>
		<rect class="a" x="6" width="2" height="2" style="fill:yellow; stroke: none"/>
		<rect class="x c" x="8" width="2" height="2"/>`
	img, report := render(t, doc(10, 2, body), 10, 2)
	for i, want := range [][4]uint8{red, {0, 0, 255, 255}, {0, 255, 0, 255}, {255, 255, 0, 255}, red} {
		wantPx(t, img, 2*i, 0, want, 0)
	}
	if len(report) != 0 {
		t.Errorf("report %q", report)
	}
}

func TestStrokesHaveWidthCapsAndJoins(t *testing.T) {
	// A horizontal line, 4 wide, from x 10 to 30.
	line := `<path d="M10 10 H30" stroke="black" stroke-width="4" stroke-linecap="%s"/>`
	for _, tc := range []struct {
		cap  string
		area float64
	}{{"butt", 80}, {"square", 96}, {"round", 80 + 4*math.Pi}} {
		img, _ := render(t, doc(40, 20, fmt.Sprintf(line, tc.cap)), 40, 20)
		wantCoverage(t, img, tc.area, 1)
	}
	// A right angle: miter fills the corner square, bevel half of it,
	// round a quarter disc.
	corner := `<path d="M2 10 H10 V2" fill="none" stroke="black" stroke-width="4" stroke-linejoin="%s"/>`
	for _, tc := range []struct {
		join string
		area float64
	}{{"miter", 60 + 4}, {"bevel", 60 + 2}, {"round", 60 + math.Pi}, {"arcs", 60 + 4}} {
		img, _ := render(t, doc(20, 20, fmt.Sprintf(corner, tc.join)), 20, 20)
		wantCoverage(t, img, tc.area, 0.6)
	}
	// A sharp spike beyond the miter limit is bevelled.
	spike := `<path d="M0 0 L20 2 L0 4" fill="none" stroke="black" stroke-width="2" stroke-miterlimit="%s"/>`
	sharp, _ := render(t, doc(40, 10, fmt.Sprintf(spike, "100")), 40, 10)
	blunt, _ := render(t, doc(40, 10, fmt.Sprintf(spike, "4")), 40, 10)
	if px(sharp, 25, 2)[3] == 0 || px(blunt, 25, 2)[3] != 0 {
		t.Errorf("miter tip at (25, 2): limit 100 %v, limit 4 %v", px(sharp, 25, 2), px(blunt, 25, 2))
	}
}

func TestAClosedStrokeIsARing(t *testing.T) {
	img, _ := render(t, doc(20, 20, `<rect x="4" y="4" width="12" height="12" fill="none" stroke="red" stroke-width="2"/>`), 20, 20)
	wantPx(t, img, 10, 10, clear, 0)
	wantPx(t, img, 4, 10, red, 0)
	wantPx(t, img, 3, 3, red, 0) // the mitred corner
	wantCoverage(t, img, 14*14-10*10, 0.5)
	img, _ = render(t, doc(20, 20, `<circle cx="10" cy="10" r="6" fill="none" stroke="red" stroke-width="2"/>`), 20, 20)
	wantPx(t, img, 10, 10, clear, 0)
	wantCoverage(t, img, math.Pi*(49-25), 1)
}

func TestDashesSplitTheStroke(t *testing.T) {
	img, _ := render(t, doc(40, 4, `<path d="M0 2 H40" stroke="black" stroke-width="2" stroke-dasharray="5 5" stroke-dashoffset="2"/>`), 40, 4)
	// Dashes over 0..3, 8..13, 18..23, ...
	wantPx(t, img, 1, 2, black, 0)
	wantPx(t, img, 5, 2, clear, 0)
	wantPx(t, img, 10, 2, black, 0)
	wantCoverage(t, img, 40, 0.5)
	// An odd list repeats; zero length or negative values mean solid.
	img, _ = render(t, doc(40, 4, `<path d="M0 2 H40" stroke="black" stroke-width="2" stroke-dasharray="4, 2, 2"/>`), 40, 4)
	wantCoverage(t, img, 2*22, 0.5)
	img, _ = render(t, doc(40, 4, `<path d="M0 2 H40" stroke="black" stroke-width="2" stroke-dasharray="0 0"/>`), 40, 4)
	wantCoverage(t, img, 80, 0.5)
}

func TestStrokesFollowTransformsAndOpacity(t *testing.T) {
	img, _ := render(t, doc(40, 20, `<path d="M5 5 H15" stroke="red" stroke-width="2" stroke-opacity="0.5" transform="scale(2)"/>`), 40, 20)
	wantPx(t, img, 20, 10, [4]uint8{128, 0, 0, 128}, 1)
	wantPx(t, img, 20, 8, [4]uint8{128, 0, 0, 128}, 1)
	wantPx(t, img, 20, 7, clear, 0)
	wantCoverage(t, img, 20*4*0.5, 0.5)
}

func TestOpacityAppliesPerPaintAndPerGroup(t *testing.T) {
	half := [4]uint8{128, 0, 0, 128}
	img, _ := render(t, doc(4, 4, `<rect width="4" height="4" fill="red" fill-opacity="0.5"/>`), 4, 4)
	wantPx(t, img, 1, 1, half, 1)
	img, _ = render(t, doc(4, 4, `<rect width="4" height="4" fill="red" opacity="50%"/>`), 4, 4)
	wantPx(t, img, 1, 1, half, 1)
	// A group's opacity applies to its composite: overlapping children do
	// not double up.
	img, _ = render(t, doc(4, 4, `<g opacity="0.5"><rect width="4" height="4" fill="red"/><rect width="2" height="2" fill="red"/></g>`), 4, 4)
	wantPx(t, img, 0, 0, half, 1)
	wantPx(t, img, 3, 3, half, 1)
	// Without a group, translucent shapes stack.
	img, _ = render(t, doc(4, 4, `<rect width="4" height="4" fill="red" fill-opacity="0.5"/><rect width="2" height="2" fill="#00f" fill-opacity="0.5"/>`), 4, 4)
	wantPx(t, img, 0, 0, [4]uint8{64, 0, 128, 191}, 1)
}

func TestLinearGradientsInterpolateAlongTheirVector(t *testing.T) {
	grad := `<defs><linearGradient id="g" %s><stop offset="0" stop-color="#000"/><stop offset="1" stop-color="#fff"/></linearGradient></defs>
		<rect x="%s" width="100" height="10" fill="url(#g)"/>`
	img, _ := render(t, doc(100, 10, fmt.Sprintf(grad, ``, "0")), 100, 10)
	wantPx(t, img, 0, 5, [4]uint8{1, 1, 1, 255}, 2)
	wantPx(t, img, 50, 5, [4]uint8{129, 129, 129, 255}, 2)
	wantPx(t, img, 99, 5, [4]uint8{254, 254, 254, 255}, 2)
	// User space units place the vector on the canvas; pad beyond it.
	img, _ = render(t, doc(100, 10, fmt.Sprintf(grad, `gradientUnits="userSpaceOnUse" x1="25" x2="75"`, "0")), 100, 10)
	wantPx(t, img, 10, 5, black, 0)
	wantPx(t, img, 50, 5, [4]uint8{128, 128, 128, 255}, 2)
	wantPx(t, img, 90, 5, [4]uint8{255, 255, 255, 255}, 0)
	// Reflect and repeat beyond it.
	img, _ = render(t, doc(100, 10, fmt.Sprintf(grad, `gradientUnits="userSpaceOnUse" x1="0" x2="25" spreadMethod="reflect"`, "0")), 100, 10)
	wantPx(t, img, 37, 5, [4]uint8{128, 128, 128, 255}, 6)
	wantPx(t, img, 49, 5, [4]uint8{0, 0, 0, 255}, 6)
	img, _ = render(t, doc(100, 10, fmt.Sprintf(grad, `gradientUnits="userSpaceOnUse" x1="0" x2="25" spreadMethod="repeat"`, "0")), 100, 10)
	wantPx(t, img, 26, 5, [4]uint8{0, 0, 0, 255}, 15)
	// A rotated gradient runs top to bottom.
	img, _ = render(t, doc(10, 100, `<linearGradient id="v" gradientTransform="rotate(90)"><stop offset="0" stop-color="red"/><stop offset="100%" stop-color="blue"/></linearGradient><rect width="10" height="100" fill="url(#v)"/>`), 10, 100)
	wantPx(t, img, 5, 1, [4]uint8{252, 0, 3, 255}, 3)
	wantPx(t, img, 5, 98, [4]uint8{3, 0, 252, 255}, 3)
}

func TestGradientsInheritThroughHrefAndUseStopOpacity(t *testing.T) {
	body := `<linearGradient id="base"><stop offset="0" stop-color="red" stop-opacity="0.5"/><stop offset="1" style="stop-color:red;stop-opacity:0.5"/></linearGradient>
		<linearGradient id="user" xlink:href="#base" x1="0" x2="1"/>
		<linearGradient id="one"><stop offset="0.5" stop-color="lime"/></linearGradient>
		<linearGradient id="none"/>
		<rect width="4" height="4" fill="url(#user)"/>
		<rect x="4" width="4" height="4" fill="url(#one)"/>
		<rect x="8" width="4" height="4" fill="url(#none)"/>`
	img, _ := render(t, doc(12, 4, body), 12, 4)
	wantPx(t, img, 1, 1, [4]uint8{128, 0, 0, 128}, 1)
	wantPx(t, img, 5, 1, [4]uint8{0, 255, 0, 255}, 0)
	wantPx(t, img, 9, 1, clear, 0)
}

func TestRadialGradientsRadiateFromTheirFocus(t *testing.T) {
	body := `<radialGradient id="r" %s><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#000"/></radialGradient>
		<rect width="100" height="100" fill="url(#r)"/>`
	img, _ := render(t, doc(100, 100, fmt.Sprintf(body, ``)), 100, 100)
	wantPx(t, img, 50, 50, [4]uint8{254, 254, 254, 255}, 3)
	wantPx(t, img, 75, 50, [4]uint8{127, 127, 127, 255}, 3)
	wantPx(t, img, 99, 50, [4]uint8{2, 2, 2, 255}, 3)
	wantPx(t, img, 0, 0, black, 0)
	img, _ = render(t, doc(100, 100, fmt.Sprintf(body, `fx="0.25"`)), 100, 100)
	wantPx(t, img, 25, 50, [4]uint8{254, 254, 254, 255}, 4)
	wantPx(t, img, 60, 50, [4]uint8{127, 127, 127, 255}, 12)
}

func TestUseDrawsTheReferencedElementOrSymbolAtItsOffset(t *testing.T) {
	body := `<defs><rect id="r" width="2" height="2" fill="red"/>
		<symbol id="s" viewBox="0 0 1 1"><rect width="1" height="1" fill="lime"/></symbol></defs>
		<use href="#r" x="4"/>
		<use xlink:href="#s" x="8" width="2" height="2"/>
		<use xlink:href="#loop" id="loop"/>`
	img, _ := render(t, doc(12, 2, body), 12, 2)
	wantPx(t, img, 0, 0, clear, 0)
	wantPx(t, img, 5, 1, red, 0)
	wantPx(t, img, 9, 1, [4]uint8{0, 255, 0, 255}, 0)
}

func TestClipPathsCutTheirElements(t *testing.T) {
	body := `<clipPath id="c"><circle cx="10" cy="10" r="5"/></clipPath>
		<clipPath id="b" clipPathUnits="objectBoundingBox"><rect width="0.5" height="1"/></clipPath>
		<rect width="20" height="20" fill="red" clip-path="url(#c)"/>
		<g clip-path="url(#b)"><rect x="20" width="20" height="20" fill="red"/></g>`
	img, report := render(t, doc(40, 20, body), 40, 20)
	wantPx(t, img, 10, 10, red, 0)
	wantPx(t, img, 1, 1, clear, 0)
	wantPx(t, img, 25, 10, red, 0)
	wantPx(t, img, 35, 10, clear, 0)
	wantCoverage(t, img, 25*math.Pi+200, 1.5)
	if len(report) != 0 {
		t.Errorf("report %q", report)
	}
}

func TestMasksScaleByLuminanceAndAlpha(t *testing.T) {
	body := `<mask id="m" maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20"><rect width="10" height="20" fill="white"/><rect x="10" width="10" height="20" fill="white" fill-opacity="0.5"/></mask>
		<rect width="20" height="20" fill="red" mask="url(#m)"/>`
	img, _ := render(t, doc(20, 20, body), 20, 20)
	wantPx(t, img, 5, 5, red, 0)
	wantPx(t, img, 15, 5, [4]uint8{128, 0, 0, 128}, 1)
}

func TestHiddenAndUndrawableElementsDrawNothing(t *testing.T) {
	for _, body := range []string{
		`<rect width="4" height="4" display="none"/>`,
		`<g style="display:none"><rect width="4" height="4"/></g>`,
		`<rect width="4" height="4" visibility="hidden"/>`,
		`<g visibility="hidden"><rect width="4" height="4"/></g>`,
		`<rect width="0" height="4"/>`,
		`<rect width="-4" height="4"/>`,
		`<circle r="0"/>`,
		`<defs><rect width="4" height="4"/></defs>`,
		`<symbol><rect width="4" height="4"/></symbol>`,
		`<path d="M 1 1 L"/>`,
		`<path d="X"/>`,
		`<linearGradient><rect width="4" height="4"/></linearGradient>`,
		`<rect width="4" height="4" fill="none" stroke="red" stroke-width="0"/>`,
	} {
		img, _ := render(t, doc(4, 4, body), 4, 4)
		if c := coverage(img); c != 0 {
			t.Errorf("%s: coverage %v, want nothing drawn", body, c)
		}
	}
	// A hidden group's visible child is drawn.
	img, _ := render(t, doc(4, 4, `<g visibility="hidden"><rect width="4" height="4" visibility="visible"/></g>`), 4, 4)
	wantPx(t, img, 1, 1, black, 0)
	// A path draws up to its first error.
	img, _ = render(t, doc(4, 4, `<path d="M0 0 H4 V4 H0 Z L x"/>`), 4, 4)
	wantPx(t, img, 1, 1, black, 0)
}

func TestUnsupportedFeaturesDegradeAndAreReported(t *testing.T) {
	body := `<defs><filter id="f"><feGaussianBlur stdDeviation="2"/></filter></defs>
		<text x="0" y="10">rite</text>
		<rect width="4" height="4" fill="red" filter="url(#f)"/>
		<rect x="4" width="4" height="4" fill="url(#f) lime"/>
		<rect x="8" width="4" height="4" fill="url(#f)"/>
		<path d="M0 0 L4 4" marker-end="url(#m)" stroke="black"/>
		<image width="4" height="4" href="x.png"/>
		<foreignObject/><bogus/>
		<rect x="12" width="4" height="4" fill="red"/>`
	img, report := render(t, doc(16, 4, body), 16, 4)
	want := []string{"<bogus>", "<foreignObject>", "<image>", "<text>", "filter", "marker"}
	if !slices.Equal(report, want) {
		t.Errorf("report %q, want %q", report, want)
	}
	wantPx(t, img, 5, 1, [4]uint8{0, 255, 0, 255}, 0) // the fallback colour
	wantPx(t, img, 13, 1, red, 0)                     // later content is drawn
}

func TestForeignNamespacesAndDoctypeEntitiesAreUnderstood(t *testing.T) {
	text := `<?xml version="1.0"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd" [
	<!ENTITY ns_svg "http://www.w3.org/2000/svg">
	<!ENTITY fill "#ff0000">
]>
<svg xmlns="&ns_svg;" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" width="4" height="4">
	<inkscape:namedview pagecolor="#fff"/>
	<metadata><title>t</title></metadata>
	<rect width="4" height="4" fill="&fill;" inkscape:label="layer"/>
</svg>`
	img, report := render(t, text, 4, 4)
	wantPx(t, img, 1, 1, red, 0)
	if len(report) != 0 {
		t.Errorf("report %q", report)
	}
	text = `<svg:svg xmlns:svg="http://www.w3.org/2000/svg" width="4" height="4"><svg:rect width="4" height="4" fill="red"/></svg:svg>`
	img, _ = render(t, text, 4, 4)
	wantPx(t, img, 1, 1, red, 0)
}

func TestBrokenOrForeignDocumentsAreRefused(t *testing.T) {
	for _, text := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><rect`,
		``,
		`<html/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="4"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 -1 4"/>`,
		`<svg xmlns="http://example.org/not-svg"/>`,
	} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%q: parsed", text)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%q: an empty reason", text)
		}
	}
}

func TestRenderDrawsOverTheDestinationThroughTheMatrix(t *testing.T) {
	d, err := Parse([]byte(doc(2, 2, `<rect width="2" height="2" fill="red" fill-opacity="0.5"/>`)))
	if err != nil {
		t.Fatal(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range dst.Pix {
		dst.Pix[i] = 255 // opaque white
	}
	d.Render(dst, Translate(2, 2).Mul(Scale(2, 2)))
	wantPx(t, dst, 0, 0, [4]uint8{255, 255, 255, 255}, 0)
	wantPx(t, dst, 3, 3, [4]uint8{255, 127, 127, 255}, 1)
	wantPx(t, dst, 6, 6, [4]uint8{255, 255, 255, 255}, 0)
}

func TestOverlappingOutlinesAreNotCountedTwiceOnEdges(t *testing.T) {
	// Two squares overlapping in x 5..10, their top edges halfway through
	// row 0: under the nonzero rule that row is half covered, overlap or
	// not.
	img, _ := render(t, doc(16, 4, `<path d="M0 0.5H10V4H0Z M5 0.5H15V4H5Z"/>`), 16, 4)
	for _, x := range []int{2, 7, 12} {
		wantPx(t, img, x, 0, [4]uint8{0, 0, 0, 128}, 1)
	}
	// A stroke's two bands overlap at its inner corner (6.5, 3.5): the
	// pixel there is three quarters covered. Union 21 + 27 − 2.25, plus
	// the miter's 2.25.
	img, _ = render(t, doc(12, 12, `<path d="M1 2 H8 V11" fill="none" stroke="black" stroke-width="3"/>`), 12, 12)
	wantCoverage(t, img, 48, 0.3)
	wantPx(t, img, 6, 3, [4]uint8{0, 0, 0, 191}, 1)
}

func TestPatternsTileTheirContent(t *testing.T) {
	green := [4]uint8{0, 255, 0, 255}
	// A 4×4 tile in user space with a red 2×2 square in its corner.
	img, report := render(t, doc(8, 8, `<pattern id="p" width="4" height="4" patternUnits="userSpaceOnUse"><rect width="2" height="2" fill="red"/></pattern>
		<rect width="8" height="8" fill="url(#p)"/>`), 8, 8)
	for _, c := range []struct {
		x, y int
		want [4]uint8
	}{{0, 0, red}, {1, 1, red}, {2, 0, clear}, {3, 3, clear}, {4, 4, red}, {5, 4, red}, {6, 6, clear}} {
		wantPx(t, img, c.x, c.y, c.want, 2)
	}
	if len(report) != 0 {
		t.Errorf("report %q", report)
	}
	// Bounding box units (the default) for the tile, a view box for the
	// content, a transform for the whole pattern, inherited through href.
	img, _ = render(t, doc(16, 8, `<pattern id="base" width=".5" height="1" viewBox="0 0 1 1" preserveAspectRatio="none"><rect width=".5" height="1" fill="lime"/></pattern>
		<pattern id="p" href="#base"/>
		<pattern id="moved" href="#base" patternTransform="translate(2 0)"/>
		<rect width="8" height="4" fill="url(#p)"/>
		<rect y="4" width="8" height="4" fill="url(#moved)"/>`), 16, 8)
	wantPx(t, img, 0, 1, green, 2)
	wantPx(t, img, 1, 1, green, 2)
	wantPx(t, img, 2, 1, clear, 2)
	wantPx(t, img, 4, 1, green, 2)
	wantPx(t, img, 2, 5, green, 2)
	wantPx(t, img, 1, 5, clear, 2)
	// An empty or zero-sized pattern paints nothing; its fallback does.
	img, _ = render(t, doc(4, 4, `<pattern id="z" width="0" height="4"/><rect width="4" height="4" fill="url(#z) red"/>`), 4, 4)
	wantPx(t, img, 1, 1, clear, 0)
}

func TestContextPaintsTakeTheUsesPaint(t *testing.T) {
	body := `<defs><path id="d" d="M0 0H4V4H0Z" fill="context-fill" stroke="context-stroke"/></defs>
		<use href="#d" fill="red" stroke="none"/>
		<use href="#d" x="4" fill="none" stroke="blue" stroke-width="2"/>
		<path d="M8 0H12V4H8Z" fill="context-fill"/>`
	img, report := render(t, doc(12, 4, body), 12, 4)
	wantPx(t, img, 1, 1, red, 0)
	wantPx(t, img, 4, 1, [4]uint8{0, 0, 255, 255}, 0)
	wantPx(t, img, 6, 1, clear, 0)
	wantPx(t, img, 9, 1, clear, 0) // no context element: nothing
	if len(report) != 0 {
		t.Errorf("report %q", report)
	}
}
