// Package text holds the fonts logis draws text with (spec §5, D2/D4):
// faces resolved by libfontconfig in-process, honouring the user's aliases,
// with per-character fallback in fontconfig's own order; only resolved
// files are ever loaded. It shapes simple runs (cmap, advances, kerning,
// one direction; the OpenType shaper of the text-shaping slice replaces
// Shape) and renders hinted glyph images through FreeType, colour emoji
// scaled with swash's filter.
//
// A Fonts and its C objects (fontconfig configuration and fallback set,
// FreeType library and faces) have exactly one owner at a time: the
// goroutine resolving them, then, handed over once through PendingFonts,
// the renderer's locked goroutine. Close releases everything; there are
// no finalizers.
package text

import (
	"strings"

	"github.com/nerdwave-nick/logis/internal/fc"
	"github.com/nerdwave-nick/logis/internal/ft"
)

// DefaultFamily is the family logis asks for when nothing else is
// configured.
const DefaultFamily = "sans-serif"

// Face is a face to resolve (a theme's): families in order of preference
// (fontconfig picks the first installed one, generic families through the
// user's aliases) at an OpenType weight (400 regular, 600 semibold, 700
// bold).
type Face struct {
	Families []string
	Weight   int
}

// Fonts is the font book: the faces fontconfig resolved, the default
// family's first, then theme faces and fallbacks as they are needed.
type Fonts struct {
	config    *fc.Config
	lib       *ft.Library
	requested string
	fallbacks *fc.FontSet
	sorted    bool
	fonts     []*Font
	seen      map[rune]bool
	// faces are the fonts texts name by index (a theme's).
	faces   []FontID
	hinting Hinting
}

// New loads the face fontconfig resolves for family from config; Fonts
// owns config from now on, also when New fails.
func New(config *fc.Config, family string) (*Fonts, error) {
	lib, err := ft.New()
	if err != nil {
		config.Close()
		return nil, &ConfigError{Why: err.Error()}
	}
	f := &Fonts{config: config, lib: lib, requested: family, seen: map[rune]bool{}, hinting: DefaultHinting}
	file, _, ok := config.Best(family)
	if !ok {
		f.Close()
		return nil, &NoFaceError{Family: family}
	}
	if _, err := f.load(file); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Close releases every font, the fallback order, the configuration and
// FreeType.
func (f *Fonts) Close() {
	for _, font := range f.fonts {
		font.close()
	}
	f.fonts = nil
	if f.fallbacks != nil {
		f.fallbacks.Close()
		f.fallbacks = nil
	}
	if f.config != nil {
		f.config.Close()
		f.config = nil
	}
	if f.lib != nil {
		f.lib.Close()
		f.lib = nil
	}
}

// Loaded is the files loaded so far, the default family's first; a
// FontID indexes it.
func (f *Fonts) Loaded() []fc.FontFile {
	files := make([]fc.FontFile, len(f.fonts))
	for i, font := range f.fonts {
		files[i] = font.File
	}
	return files
}

// Font is a loaded font.
func (f *Fonts) Font(id FontID) *Font { return f.fonts[id] }

// Family is the default face's family name.
func (f *Fonts) Family() string { return f.fonts[0].Family }

// SetFaces resolves faces through fontconfig (FcFontMatch per face),
// loading each resolved file once, and makes them the faces texts name by
// index. A face that resolves to nothing readable is the default family
// instead; the first such failure is returned once all are resolved.
func (f *Fonts) SetFaces(faces []Face) error {
	var first error
	f.faces = make([]FontID, 0, len(faces))
	for _, face := range faces {
		id, err := f.resolve(face)
		if err != nil && first == nil {
			first = err
		}
		f.faces = append(f.faces, id)
	}
	return first
}

func (f *Fonts) resolve(face Face) (FontID, error) {
	file, _, ok := f.config.BestOf(face.Families, face.Weight)
	if !ok {
		return 0, &NoFaceError{Family: strings.Join(face.Families, ", ")}
	}
	return f.load(file)
}

// Face is the family name and weight of face index as the shaper uses
// it: the first face for an index out of range, the default family when
// there are no faces.
func (f *Fonts) Face(index int) (family string, weight int) {
	font := f.fonts[f.face(index)]
	return font.Family, font.Weight
}

// face is the font of face index.
func (f *Fonts) face(index int) FontID {
	switch {
	case index >= 0 && index < len(f.faces):
		return f.faces[index]
	case len(f.faces) > 0:
		return f.faces[0]
	}
	return 0
}

// Ensure makes sure every character of s has a face if fontconfig knows
// one: for each character not seen before, the first face in fontconfig's
// fallback order that covers it is loaded (once). fontconfig is never
// queried per character: the order is sorted once and only its coverage
// is read.
func (f *Fonts) Ensure(s string) {
	for _, r := range s {
		if Ignorable(r) || f.seen[r] {
			continue
		}
		f.seen[r] = true
		f.sortFallbacks()
		if f.fallbacks == nil {
			continue
		}
		if file, ok := f.fallbacks.FirstCovering(r); ok {
			_, _ = f.load(file) // an unreadable fallback just leaves r uncovered
		}
	}
}

// sortFallbacks sorts fontconfig's fallback order for the family, once.
func (f *Fonts) sortFallbacks() {
	if !f.sorted {
		f.fallbacks = f.config.Fallbacks(f.requested)
		f.sorted = true
	}
}

// load is the font of file, opened now unless it is loaded already.
func (f *Fonts) load(file fc.FontFile) (FontID, error) {
	for i, font := range f.fonts {
		if font.File == file {
			return FontID(i), nil
		}
	}
	font, err := openFont(f.lib, file)
	if err != nil {
		return 0, err
	}
	f.fonts = append(f.fonts, font)
	return FontID(len(f.fonts) - 1), nil
}

// Ignorable reports characters that never need a face of their own:
// controls, joiners, variation selectors and other default-ignorables.
// Asking fontconfig for them would load whichever font happens to list
// them; the shaper hides them.
func Ignorable(r rune) bool {
	switch {
	case r < 0x20 || r >= 0x7f && r < 0xa0:
		return true
	case r == 0xad, r == 0x34f, r == 0x61c, r >= 0x180b && r <= 0x180f,
		r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2060 && r <= 0x206f,
		r >= 0xfe00 && r <= 0xfe0f, r == 0xfeff, r >= 0xe0000 && r <= 0xe0fff:
		return true
	}
	return false
}
