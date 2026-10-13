package theme

// A theme file as written (Rust src/theme/doc.rs): every key optional,
// each value checked for its kind while it is read, unknown keys refused.
// The sections and their keys are listed here in Rust's declaration order,
// which is the order serde's "expected one of …" names them in; a parsed
// file is a node of the values given.

// kind is what a key's value is.
type kind int

const (
	kText     kind = iota // a string
	kFloat                // f32
	kUint                 // u32
	kFlag                 // a boolean
	kColour               // a colour reference
	kPaint                // a paint: a colour or 1 to 4 stops
	kColours              // an array of colours
	kFamilies             // a family or an array of families
	kCaret                // bar, block or underscore
	kBadge                // outline, fill or bracket
	kSection              // a section of keys
	kToggle               // false, or a section's settings (or a pair)
	kPalette              // named colours
	kRoles                // font roles: name → FontDoc
)

// key is one key of a section.
type key struct {
	name string
	kind kind
	// sub is a section's (or a part's) keys; a toggle without one is a
	// pair of lengths (rule.dash).
	sub *section
}

// section is a struct of optional keys; name is Rust's struct name, which
// serde's "expected struct …" says.
type section struct {
	name string
	keys []key
}

func (s *section) key(name string) (key, bool) {
	for _, k := range s.keys {
		if k.name == name {
			return k, true
		}
	}
	return key{}, false
}

// node is a section as written: its keys' values, absent where not given.
// Values are string, float32, uint32, bool, colourRef, paint, []colourRef,
// []string (families), CaretShape, BadgeStyle, node, toggle, palette and
// map[string]node (font roles).
type node map[string]any

// toggle is a part of a theme that may be turned off: false, or its
// settings (missing ones come from the base).
type toggle struct {
	off bool
	// on is a part's settings while on; pair is rule.dash's lengths.
	on   node
	pair [2]float32
}

func sec(name string, keys ...key) *section { return &section{name: name, keys: keys} }

func flt(name string) key  { return key{name: name, kind: kFloat} }
func ms(name string) key   { return key{name: name, kind: kUint} }
func col(name string) key  { return key{name: name, kind: kColour} }
func txt(name string) key  { return key{name: name, kind: kText} }
func pnt(name string) key  { return key{name: name, kind: kPaint} }
func flag(name string) key { return key{name: name, kind: kFlag} }

func part(name string, s *section) key { return key{name: name, kind: kSection, sub: s} }
func off(name string, s *section) key  { return key{name: name, kind: kToggle, sub: s} }

// fontDoc is a font role.
var fontDoc = sec("FontDoc", key{name: "family", kind: kFamilies}, flt("size"), ms("weight"), flt("tracking"))

// docSection is a whole theme file.
var docSection = sec("Doc",
	txt("$schema"),
	txt("extends"),
	key{name: "palette", kind: kPalette},
	key{name: "fonts", kind: kRoles, sub: fontDoc},
	part("window", sec("WindowDoc", flt("width"), flt("height"), ms("rows"), flt("margin"))),
	part("glow", sec("GlowDoc", col("color"), flt("radius"), flt("pulse"), ms("period-ms"))),
	part("border", sec("BorderDoc", flt("width"), pnt("paint"),
		off("highlight", sec("HighlightDoc", col("color"), flt("length"), ms("period-ms"))))),
	part("panel", sec("PanelDoc", pnt("fill"), flt("radius"), flt("padding"), flag("blur"))),
	part("decorations", sec("DecorationsDoc",
		off("rivets", sec("RivetsDoc", flt("size"), flt("inset"), pnt("top"), pnt("bottom"))))),
	part("prompt", sec("PromptDoc", flt("height"), txt("label"), txt("font"), col("color"), flt("gap"),
		off("sigil", sec("SigilDoc", txt("image"), flt("size"), col("color"))))),
	part("query", sec("QueryDoc", txt("font"), col("color"), col("placeholder"),
		part("caret", sec("CaretDoc", key{name: "shape", kind: kCaret}, flt("width"), col("color"), col("glow"))))),
	part("counter", sec("CounterDoc", txt("font"), col("color"), txt("format"))),
	part("rule", sec("RuleDoc", pnt("paint"), flt("thickness"), flt("inset"), key{name: "dash", kind: kToggle},
		off("ornament", sec("OrnamentDoc", flt("size"), col("fill"), col("edge"))))),
	part("row", sec("RowDoc", txt("font"), flt("height"), flt("gap"), flt("padding"),
		key{name: "columns", kind: kColours}, col("glyph"), flt("column-gap"), flt("column-max"))),
	part("selection", sec("SelectionDoc", pnt("band"), col("outline"), col("edge"), col("glow"),
		key{name: "columns", kind: kColours}, flt("inset"))),
	part("match", sec("MatchDoc", col("color"), col("selected"), ms("weight"), flag("underline"))),
	part("badge", sec("BadgeDoc", txt("font"), key{name: "style", kind: kBadge}, col("ink"), col("edge"),
		col("fill"), flag("uppercase"))),
	part("pending", sec("PendingDoc", col("color"), flt("opacity"), flt("shimmer"), ms("period-ms"), txt("mark"))),
	part("effects", sec("EffectsDoc", flt("scanlines"), flt("grain"), flt("flicker"), flt("vignette"),
		flt("bloom"), flt("curvature"), flt("aberration"))),
	part("motion", sec("MotionDoc", ms("open-ms"), ms("close-ms"), ms("select-ms"))),
)

// The words of the enums, as serde lists them.
var (
	caretWords = []string{"bar", "block", "underscore"}
	badgeWords = []string{"outline", "fill", "bracket"}
)
