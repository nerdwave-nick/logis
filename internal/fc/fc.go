// Package fc is logis's thin binding to libfontconfig (spec §5, D4), the
// same few calls as Rust src/text/fc.rs: load a configuration, substitute
// and match a family list at a weight, sort one fallback order, and read
// file, index, family and coverage from the resulting patterns.
//
// Every C object is owned by exactly one Go value and freed by its Close,
// never by a finalizer. fontconfig has no thread affinity and is
// thread-safe since 2.10.91; a Config or FontSet still has one owner at a
// time (the goroutine resolving fonts, then the one drawing them).
package fc

/*
#cgo pkg-config: fontconfig
#include <fontconfig/fontconfig.h>
#include <stdlib.h>

static FcPattern *logis_fc_font_at(FcFontSet *s, int i) { return s->fonts[i]; }
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// FontFile is one face fontconfig resolved: a file and the face in it
// (0 unless the file is a collection).
type FontFile struct {
	Path  string
	Index int
}

// Config is a loaded fontconfig configuration: its rules (aliases
// included) and the fonts it knows. Only metadata; no font is opened here.
type Config struct{ c *C.FcConfig }

var (
	objFamily  = C.CString("family")
	objFile    = C.CString("file")
	objIndex   = C.CString("index")
	objCharset = C.CString("charset")
	objWeight  = C.CString("weight")
)

// System is the user's configuration exactly as other fontconfig clients
// see it: system and user fonts.conf, aliases and the font cache.
func System() (*Config, error) {
	c := C.FcInitLoadConfigAndFonts()
	if c == nil {
		return nil, errors.New("fontconfig would not load")
	}
	return &Config{c}, nil
}

// Isolated is a configuration with only the rules in xml (a <fontconfig>
// document) and only the font files given, nothing from the system.
func Isolated(xml string, files []string) (*Config, error) {
	c := C.FcConfigCreate()
	if c == nil {
		return nil, errors.New("fontconfig could not create a configuration")
	}
	cfg := &Config{c}
	if strings.ContainsRune(xml, 0) {
		cfg.Close()
		return nil, errors.New("the rules contain a NUL byte")
	}
	rules := C.CString(xml)
	ok := C.FcConfigParseAndLoadFromMemory(c, (*C.FcChar8)(unsafe.Pointer(rules)), C.FcTrue)
	C.free(unsafe.Pointer(rules))
	if ok != C.FcTrue {
		cfg.Close()
		return nil, errors.New("fontconfig rejected the rules")
	}
	for _, file := range files {
		if strings.ContainsRune(file, 0) {
			cfg.Close()
			return nil, fmt.Errorf("%s contains a NUL byte", file)
		}
		name := C.CString(file)
		ok := C.FcConfigAppFontAddFile(c, (*C.FcChar8)(unsafe.Pointer(name)))
		C.free(unsafe.Pointer(name))
		if ok != C.FcTrue {
			cfg.Close()
			return nil, fmt.Errorf("fontconfig could not read %s", file)
		}
	}
	return cfg, nil
}

// Close releases the configuration. Patterns and sets handed out are
// independent copies and stay valid.
func (c *Config) Close() {
	if c.c != nil {
		C.FcConfigDestroy(c.c)
		c.c = nil
	}
}

// Best is the best face for family after substitution (aliases,
// defaults), with its family name as fontconfig knows it.
func (c *Config) Best(family string) (FontFile, string, bool) {
	return c.BestOf([]string{family}, 0)
}

// BestOf is the best face for the first of families that is installed
// (matched as one list, in order, as fontconfig clients do), at the
// OpenType weight if it is above 0, with its family name.
func (c *Config) BestOf(families []string, weight int) (FontFile, string, bool) {
	p := c.query(families, weight)
	if p == nil {
		return FontFile{}, "", false
	}
	defer C.FcPatternDestroy(p)
	var result C.FcResult
	found := C.FcFontMatch(c.c, p, &result)
	if found == nil {
		return FontFile{}, "", false
	}
	defer C.FcPatternDestroy(found)
	file, ok := fileOf(found)
	if !ok {
		return FontFile{}, "", false
	}
	family, ok := stringOf(found, objFamily)
	return file, family, ok
}

// Fallbacks is every face that adds coverage for family, best first:
// fontconfig's own fallback order (FcFontSort, trimmed), honouring the
// configured aliases. Nil if fontconfig has none.
func (c *Config) Fallbacks(family string) *FontSet {
	p := c.query([]string{family}, 0)
	if p == nil {
		return nil
	}
	defer C.FcPatternDestroy(p)
	var result C.FcResult
	set := C.FcFontSort(c.c, p, C.FcTrue, nil, &result)
	if set == nil {
		return nil
	}
	return &FontSet{set}
}

// query is a pattern for families (in order) at weight with the
// configuration's substitutions applied; nil for a family fontconfig
// cannot take (a NUL byte in it).
func (c *Config) query(families []string, weight int) *C.FcPattern {
	p := C.FcPatternCreate()
	if p == nil {
		return nil
	}
	for _, family := range families {
		if strings.ContainsRune(family, 0) {
			C.FcPatternDestroy(p)
			return nil
		}
		s := C.CString(family)
		ok := C.FcPatternAddString(p, objFamily, (*C.FcChar8)(unsafe.Pointer(s)))
		C.free(unsafe.Pointer(s))
		if ok != C.FcTrue {
			C.FcPatternDestroy(p)
			return nil
		}
	}
	if weight > 0 && C.FcPatternAddInteger(p, objWeight, C.FcWeightFromOpenType(C.int(weight))) != C.FcTrue {
		C.FcPatternDestroy(p)
		return nil
	}
	C.FcConfigSubstitute(c.c, p, C.FcMatchPattern)
	C.FcDefaultSubstitute(p)
	return p
}

// FontSet is an owned, sorted list of faces (a fallback order).
type FontSet struct{ s *C.FcFontSet }

// FirstCovering is the first face in the list whose coverage includes r.
func (s *FontSet) FirstCovering(r rune) (FontFile, bool) {
	for i := range int(s.s.nfont) {
		p := C.logis_fc_font_at(s.s, C.int(i))
		var cs *C.FcCharSet
		if C.FcPatternGetCharSet(p, objCharset, 0, &cs) == C.FcResultMatch &&
			C.FcCharSetHasChar(cs, C.FcChar32(r)) == C.FcTrue {
			return fileOf(p)
		}
	}
	return FontFile{}, false
}

// Close releases the set and every pattern in it.
func (s *FontSet) Close() {
	if s.s != nil {
		C.FcFontSetDestroy(s.s)
		s.s = nil
	}
}

func fileOf(p *C.FcPattern) (FontFile, bool) {
	path, ok := stringOf(p, objFile)
	if !ok {
		return FontFile{}, false
	}
	var index C.int // a missing index means face 0
	C.FcPatternGetInteger(p, objIndex, 0, &index)
	// The upper 16 bits name a variable font's instance; the face is below.
	return FontFile{Path: path, Index: int(index) & 0xffff}, true
}

func stringOf(p *C.FcPattern, object *C.char) (string, bool) {
	var s *C.FcChar8
	if C.FcPatternGetString(p, object, 0, &s) != C.FcResultMatch || s == nil {
		return "", false
	}
	return C.GoString((*C.char)(unsafe.Pointer(s))), true
}
