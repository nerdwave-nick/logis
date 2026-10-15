package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The panel's height follows its rows (a divergence from Rust v1, whose
// 520-pixel window left an empty band of about three rows below the ninth
// row): a dmenu with more items than rows and no --lines shows the theme's
// eleven rows, or on an output too short for them the rows that fit,
// spread over its height, and the body ends right after the last row,
// with nothing below it but the theme's row gap. Measured in the capture,
// not in logis's own numbers: the rows are the bands of bright text in the
// body's left part, the body's bottom the first crimson pixel (border or
// glow) below the last row in its right part, where no row text reaches.
func TestE2EThePanelEndsAfterItsLastRow(t *testing.T) {
	for _, s := range []string{"2", "2.5"} {
		t.Run("scale "+s, func(t *testing.T) { checkRowFit(t, s) })
	}
}

func checkRowFit(t *testing.T, scaleText string) {
	c := e2e.StartCogitator(t, nil, "", e2e.Scale(scaleText))
	var input strings.Builder
	for i := range 60 {
		fmt.Fprintf(&input, "rite %02d\n", i+1)
	}
	width := uint32(30) // narrow enough for the nested output at any scale
	client := c.DmenuAll([]byte(input.String()), "--width="+strconv.Itoa(int(width)))
	focused := c.Shown(60)
	c.OpenSettled()
	capture := c.Harness.SettledCapture()
	if dir := os.Getenv("LOGIS_E2E_SHOTS"); dir != "" {
		_ = rendertest.WritePNG(filepath.Join(dir, "rowfit@"+scaleText+".png"), capture)
	}

	sc, err := strconv.ParseFloat(scaleText, 64)
	if err != nil {
		t.Fatal(err)
	}
	_, physical := c.Harness.OutputSize()
	room := uint32(math.Round(float64(physical) / sc))
	th := theme.Default()
	o := list.DefaultOptions()
	o.Width = &width
	size, rows := scene.FitIn(o, &th, room)
	pitch := scene.GridOf(size, rows, &th).Pitch
	t.Logf("output %d logical px tall: %v with %d rows at %.2f", room, size, rows, pitch)
	if room >= uint32(2*th.Window.Margin+th.Prompt.Height+th.Rule.Thickness+2*th.Row.Gap+11*th.Row.Height) && rows != 11 {
		t.Fatalf("%d rows on an output %d tall, want the theme's 11", rows, room)
	}

	// The body in the capture, centred on the output.
	bodyW := (float64(size.W) - 2*float64(th.Window.Margin)) * sc
	left := (float64(capture.Width) - bodyW) / 2
	textFrom, textTo := int(left+float64(th.Row.Padding)*sc), int(left+bodyW*0.4)
	probe := int(left + bodyW*0.85)
	bands := textBands(capture, textFrom, textTo)
	if len(bands) < int(rows) {
		t.Fatalf("%d text bands, want at least %d rows: %v", len(bands), rows, bands)
	}
	// The rows: bands starting a pitch apart, up from the last one (the
	// selected row's band may split its text into more bands).
	startsAt := func(y float64) bool {
		for _, b := range bands {
			if math.Abs(float64(b[0])-y) <= 2*sc {
				return true
			}
		}
		return false
	}
	lastTop := float64(bands[len(bands)-1][0])
	shown := 0
	for startsAt(lastTop - float64(shown)*float64(pitch)*sc) {
		shown++
	}
	if shown != int(rows) {
		t.Fatalf("%d rows a pitch (%.2f) apart, want %d: bands %v", shown, pitch, rows, bands)
	}
	last := bands[len(bands)-1][1]
	bottom := -1
	for y := last + 1; y < capture.Height; y++ {
		if r, _, _ := pixel(capture, probe, y); r > 90 {
			bottom = y
			break
		}
	}
	if bottom < 0 {
		t.Fatalf("no body bottom below the last row (at %d)", last)
	}
	// Below the last row's text: half the row's free height and the gap,
	// never another row's room (Rust v1 left about three).
	below := float64(bottom-last) / sc
	t.Logf("%.1f logical px from the last row's text to the body's bottom", below)
	if want := float64(th.Row.Gap + pitch/2); below > want || below < float64(th.Row.Gap) {
		t.Fatalf("%.1f px below the last row's text, want between the gap %v and %.1f", below, th.Row.Gap, want)
	}
	c.Tap(focused, e2e.KeyEsc)
	if got := client.Ended(); got.Code != 1 {
		t.Fatalf("%+v", got)
	}
}

// textBands are the runs of pixel rows with bright text between x from and
// to, top down, as [first, last] rows.
func textBands(img *render.Image, from, to int) [][2]int {
	var bands [][2]int
	open := false
	for y := range img.Height {
		bright := false
		for x := max(from, 0); x < min(to, img.Width) && !bright; x++ {
			r, g, b := pixel(img, x, y)
			bright = 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 140
		}
		switch {
		case bright && !open:
			bands = append(bands, [2]int{y, y})
			open = true
		case bright:
			bands[len(bands)-1][1] = y
		default:
			open = false
		}
	}
	return bands
}

func pixel(img *render.Image, x, y int) (r, g, b uint8) {
	i := (y*img.Width + x) * 4
	return img.Pixels[i], img.Pixels[i+1], img.Pixels[i+2]
}
