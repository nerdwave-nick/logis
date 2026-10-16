# Research: text, fonts and keyboard in Go (stdlib + golang.org/x)

Date: 2026-10-12. Ticket: lit `feat/logis-go` d7ee66f7 "Research: Go text, fonts and keyboard under stdlib + golang.org/x" (spec §8 D2, D3, D4).
Branch `research/go-text-input` from `main` 2ffdd02. Machine: Arch Linux, kernel 7.2.3, niri 26.04, 22 logical CPUs, Go 1.27.2, libxkbcommon 1.13.2, fontconfig 2.18.3, eDP-1 at scale 1.3.
Prototypes (disposable, each its own Go module): `research/go/text/` (D2), `research/go/xkb/` (D3), `research/go/fonts/` (D4).

Source tags: **[src]** source read at the stated version, **[local]** observed or measured here, **[test]** asserted by a test in the prototype. Statements marked *Inference* are estimates, not measurements.

Measurements ran as single commands under the shared lock `/home/nerdwave/.claude/jobs/88752f6d/tmp/measure.lock`, with `/proc/loadavg` recorded before and after (raw output in each prototype's `results/`). Another worker (W1, rendering) measured in parallel between my rounds, and several agents were building, so **load averages were 6–10 on 22 CPUs during most rounds, and one D4 round climbed to 29**. Treat absolute times as upper-side, and compare options within a round.

## Verdict

**D2 text: hand-roll an OpenType shaping subset on golang.org/x/image/font/sfnt; ship unhinted at first.** x/image gives outlines (TrueType and CFF), cmap, advances, a narrow kerning helper and an anti-aliasing rasteriser. Everything else is hand-rolled, and x/text offers no segmentation at all. A prototype shapes the whole corpus glyph-for-glyph like HarfBuzz and places ink where the hinted reference renders of the corpus do, to the pixel. The one remaining visible difference is **hinting**: the references are hinted (TrueType bytecode and CFF hinting through swash/skrifa). Own hinting would cost 30–40+ days, so it is deferred. (The owner then chose hinting through the system FreeType instead: [ADR 0002](../adr/0002-go-and-a-small-cgo-set.md), D2.) Colour emoji need only CBDT, because the owner's emoji font has no COLR/sbix/SVG.

**D3 keyboard: hand-roll it in pure Go.** A client-side XKB of about 2 400 lines (plus generated keysym tables) matches libxkbcommon 1.13.2 exactly on everything a Wayland client needs. That covers 76 keymaps, about 7.6 M key states, every sequence of 9 locale Compose tables, and the feat/logis E2E keyboard scenarios replayed from a nested niri. cgo libxkbcommon stays the test oracle only. The prior-art xkb-go is not a usable basis: it is a third-party module, and it has the defects listed in §D3.3.

**D4 fonts: `fc-match` subprocesses in the resident daemon if the binary stays `CGO_ENABLED=0`; cgo libfontconfig only if D1 links C anyway; do not port fontconfig.** A pure-Go port of fonts.conf parsing, matching and sorting was prototyped and is exact on the owner's real configuration: 0 mismatches over every theme query, every fallback character, 1 152 family×weight queries and the full 2 483-face sort orders. The trouble is upkeep: it is a port of fontconfig's internals (cache format v9 → v11 → v12 and new matching rules, all within 2026), so it is the most code and the most maintenance for no correctness gain.

## D2 Text

The D2 sub-study (prototype `research/go/text/`, module `logis.research/go/text`, `CGO_ENABLED=0`, x/image v0.47.0 + x/text v0.43.0) was run by a delegated sub-worker of this ticket. I spot-checked its source citations and re-ran its checks (`gofmt -l` empty, `go vet` clean, `go test ./...` green in 7 packages).

### D2.1 What golang.org/x provides

**x/image/font/sfnt** [src, test]
- Parses `.ttf`, CFF `.otf` (CID-keyed too, `postscript.go:84`), `.ttc`, dfont. cmap formats 0/4/6/12 (`cmap.go:91–249`); no format 14, so no variation-sequence lookup. hmtx advances, name, post, hhea metrics (`sfnt.go:1739`). **Unhinted** outlines as segments: quadratic for TrueType, cubic for CFF (機: 98 segments, 45 cubic). The CJK `.otf` loads fine.
- It knows 14 tables (`sfnt.go:846–880`). It has **no GSUB, no GDEF, no COLR/CPAL, no sbix, no SVG**, no fvar/gvar (no variable fonts), and no public raw-table accessor. The prototype has a 30-line table-directory reader. CBDT is detected, but decoding is a TODO (`sfnt.go:1186–1190`), and `LoadGlyph` returns `ErrColoredGlyph` (`sfnt.go:99`).
- **Kerning:** `Kern` (`sfnt.go:1663`, `gpos.go:21–158`) reads only GPOS lookup type 2 for Latin/default script with an X-advance on the first glyph, skips mark-filtering lookups (`gpos.go:118`) and ignores lookup flags, then falls back to `kern`. It agrees with full GPOS on A/V, but misses the chained-context kerning in the system Noto Sans.
- **Hinting:** `font.Hinting` only rounds advances, kerns and metrics, and only `HintingFull` does that (`sfnt.go:1605…1748`). Outlines are never hinted (`sfnt.go:1421, 1475`).

**x/image/font/opentype** is a rune-keyed `font.Face` without glyph ids or shaping. Bug: `Face.Kern` passes units-per-em as the pixel size (`opentype.go:145`), so A/V kerns by the same amount at 8 px and 64 px [test]. Not usable for logis.

**x/image/vector** gives anti-aliased coverage (SIMD via x/sys/cpu), with no gamma or LCD options. **x/image/draw** scalers (NearestNeighbor, ApproxBiLinear, BiLinear, CatmullRom, custom kernels) are not good enough for emoji (§D2.3).

**x/text/unicode/bidi** [src, test]
- `Paragraph.Order()` (`bidi.go:213`) returns runs in logical order labelled only LTR/RTL. Embedding levels are lost, so the visual order cannot be rebuilt: "42" at level 2 merges with " abc" at level 0. `Run.Pos` counts characters, not bytes, with an inclusive end.
- **Bug: bracket pairing never matches.** `prepareInput` stores each closing bracket's own rune (`bidi.go:113–122`), and `matchOpener` compares the runes directly (`bracket.go:124`), so `(` never equals `)`. In the corpus line this leaves `)` LTR, inside "42) ok".
- The prototype forks `core.go`/`bracket.go` verbatim (BSD-3, `LICENSE.x-text` kept, about 1 430 lines). It fixes the pairing and exposes levels and visual reordering; the corpus line then orders correctly [test]. Mirroring beyond paired brackets needs a small table.

**x/text segmentation: none.** No grapheme, word, line or sentence breaking. The packages are cases, collate, currency, date, encoding/*, feature/plural, language, message, number, runes, search, secure/{bidirule,precis}, transform, unicode/{bidi,cldr,norm,rangetable,runenames}, width. Joining_Type exists only in unexported tables (`secure/precis/context.go:61`, `internal/export/idna`). `unicode/norm` does provide canonical combining classes and decomposition. **Go 1.27 stdlib** `unicode` is Unicode 17.0.0, but it has no Grapheme_Cluster_Break, Extended_Pictographic, InCB or Joining_Type tables [src].

**Fonts in play.** The pinned `NotoColorEmoji.ttf` and the owner's system `/usr/share/fonts/noto/NotoColorEmoji.ttf` (10.4 MB) are both **CBDT/CBLC**: one PNG strike at 109 ppem, with no COLR, sbix or SVG. The system font's GSUB uses ligature, context and chained-context lookups. Noto Sans, Hebrew and Arabic carry TrueType hinting programs [local].

**How the hinted reference renders were made** [src]: cosmic-text 0.19 shapes with harfrust 0.5.2 (a full HarfBuzz port). swash 0.2.10 **hints every outline** through skrifa in Smooth mode with preserved metrics: the TrueType bytecode interpreter for Noto Sans/Hebrew/Arabic and the CFF hinter for CJK, with the autohinter only for unhinted fonts. Advances are rounded to whole pixels, and glyphon blends in sRGB space on an `Rgba8Unorm` target. Emoji are scaled with swash's Mitchell filter (negative lobes folded positive).

### D2.2 What the corpus needs from the fonts

| Font | Lookups that fire on `tests/fonts/corpus.txt` |
|---|---|
| Noto Sans | GPOS 2 pair kerning (54×). `liga` has only ff/fi/fl/ffi/ffl, and **the corpus triggers no Latin ligature** ("office fit" does, and matches HarfBuzz) |
| Noto Sans Hebrew | GPOS 4 mark-to-base (3), pair kerning (1); marks reordered with HarfBuzz's modified combining classes (shin dot before qamats) |
| Noto Sans Arabic | joining forms via **GSUB 2** (multiple substitution: body + dots, 14×), GPOS 4 for the dots (2), kerning (1); no `isol` feature; `rlig` is GSUB 5 and not triggered |
| Noto Sans CJK JP (CFF) | nothing: cmap + advances |
| Noto Color Emoji | GSUB 4 ligatures for 👍🏽 and 🧑‍💻 (2); VS16 maps to no glyph and is hidden |

The full system fonts use more: GSUB 1–6; GPOS 2, 4, 5 and 6; GPOS 8 inside Noto Sans `kern`; chained-context `rlig` in Arabic; context lookups in the emoji font. So the shaper must be the generic OpenType engine, not a corpus-specific subset.

### D2.3 Proof of concept and quality

`research/go/text` (`cmd/corpus`) renders the corpus golden scene (330×184 logical, 16 px, lines at y = 6 + 24 i, plus the ellipsis and clip rows) at 1.0, 1.3 and 2.0. It uses the pinned fonts, sRGB-space blending as glyphon does, and advances rounded to pixels.

- **Shaping:** all 25 corpus runs plus 3 extra runs give **identical glyph ids and positions to HarfBuzz 14.6.0** [test: `internal/shape`, oracle data produced at development time by the system libharfbuzz through Python ctypes, `testdata/hbref.py`; not a Go dependency].
- **Against the hinted reference renders** ([`text-corpus-compare@1.0.png`](../evidence/text-corpus-compare@1.0.png), [`@1.3`](../evidence/text-corpus-compare@1.3.png), [`@2.0`](../evidence/text-corpus-compare@2.0.png) in `docs/evidence/`: reference on top, the prototype below, ×4 difference heatmap at the bottom): horizontal ink extents match **to the pixel on all 7 lines at all 3 scales**, except 1 px on line 2 at 2.0.
  - Mean absolute difference per line (/255): Latin 4.6/4.5/4.9; accents 4.2/4.1/4.9; CJK 6.0/2.5/3.2; bidi 2.9/2.4/2.2; emoji line 2.2/2.1/2.5; ellipsis row 1.4/1.3/1.6; clip row 1.5/1.3/1.8 (at 1.0/1.3/2.0).
  - Whole image: 2.96/2.37/2.74.
  - Pixels outside the golden tolerance (2 levels): 10.4 %/8.3 %/6.2 %. **Unhinted output cannot pass a golden test against hinted references**, so unhinted text needs its own goldens.
- **Emoji scaling:** a port of swash's filter is within max 2–3 levels of the reference. x/image `CatmullRom` is off by up to 42 levels and `ApproxBiLinear` by up to 141 (`results/emoji-filter.txt`).
- **Hinting is the remaining difference.** The reference's horizontal stems and edges are sharp; the prototype's are soft, most visibly in Latin and CJK at 1.0–1.3. A 68-line stand-in that snaps baseline, x-height and cap-height made Latin about 20 % closer at 1.0/1.3 but 14 % worse at 2.0, so it was rejected (`results/corpus-compare-ysnap.txt`).
- **Grapheme editing:** the hand-rolled UAX #29 breaker (GB3–GB13 incl. GB9c, tables generated from Unicode 17.0.0 data) passes all 766 `GraphemeBreakTest.txt` cases. Backspace removes e + U+0301, 👍🏽, 🧑‍💻, 🇩🇪, ⚙️ and pointed Hebrew in one press [test].

### D2.4 Measurements

`cmd/bench` at scale 1.3. One frame is the 7 corpus lines: 215 glyphs, 87 distinct glyph images. Cold = empty glyph cache; warm = reshape every line, glyph images cached (pessimistic: reshaping only changed lines does less). N = 100 per round, each round one locked command, median/p95 in µs (`results/bench-*.txt`, commands in `results/commands.txt`):

| Round | loadavg before → after | cold frame | warm frame | shaping only | drawing only |
|---|---|---|---|---|---|
| run3, unpinned | 8.24 → 8.14 | 4 176 / 10 473 | 374 / 1 677 | 214 / 425 | 87 / 165 |
| run2, `taskset -c 0-11` | 8.70 → 8.70 | 4 077 / 9 245 | 320 / 891 | 189 / 1 353 | 91 / 117 |
| run4, `GOMAXPROCS=1 taskset -c 2` | 8.14 → 8.14 | 16 455 / 22 940 | 989 / 3 361 | 564 / 1 649 | 738 / 1 155 |
| run1 (cores throttled to 500–600 MHz during the round) | 7.74 → 8.08 | 33 133 / 43 571 | 2 475 / 3 570 | 1 332 / 2 557 | 642 / 896 |

The first cold frame in a process took 3.6–3.7 ms, dominated by decoding and scaling the 6 emoji PNGs (unlocked profile, qualitative only). Font parsing (sfnt + layout tables) from memory, median/p95 in µs (run3): NotoSans subset 20.6/66.2; CJK subset 12.2/20.3; system NotoSans (607 KB) 30.2/224.1; system NotoColorEmoji (10.4 MB) 5.3/13.8; system NotoSansCJK `.ttc` face 0 (18 MB, sfnt only) 284/512. A warm 0.3–0.4 ms text frame fits the "keystroke → frame within one frame" budget comfortably on this machine. Load was ~8 throughout, so these are upper-side numbers.

### D2.5 Effort and quality per item

*Inference* (one developer, production quality), from the prototype's LOC:

| Item | Prototype (LOC) | Quality reached | Production LOC | Days | Risk |
|---|---|---|---|---|---|
| Font loading: table directory, metrics, TTC | yes (138) | baselines identical | 250 | 1 | low |
| OpenType layout core: coverage, GDEF, lookup flags, mark sets, feature stages | yes, no ZWJ/ZWNJ skip rules (~450) | — | 1 200 | 6 | medium |
| GSUB 1/2/4/5/6/7 (3, 8 missing) | yes (~330) | = HarfBuzz | 800 | 4 | medium |
| GPOS 1/2/4/5/6/9 (3, 7, 8, device tables missing) | partly (206) | = HarfBuzz | 700 | 4 | medium |
| Normalisation, mark reordering, clusters | partly (77) | = HarfBuzz | 300 | 2 | low–medium |
| Itemisation, per-cluster fallback, script detection | simple (~120) | same faces as cosmic-text on the corpus | 400 + fontconfig (D4) | 3 | medium |
| Bidi (x/text fork + wrapper + mirroring table) | yes (75 + 1 431 fork) | corpus correct | 150 + fork | 2 | low |
| Arabic joining (generated Joining_Type) | yes (67) | = HarfBuzz | 200 | 1.5 | low |
| CBDT emoji with swash-identical scaling | yes (239) | max diff 2–3 | 400 | 2 | low |
| COLRv0 / COLRv1 / sbix | no | not needed for the owner's emoji font | 300 / 2 000 / 150 | 2 / 12 / 1 | low / high / low |
| Rasteriser, glyph cache/atlas, sRGB blending | yes (320) | positions pixel-exact | 600 | 3 | low |
| **TrueType hinting** (bytecode, Smooth) | no | Latin lines stay 4.2–4.9 off | ~3 500 (skrifa: 9.4 k) | 20–30 | high |
| **CFF hinting** (needs own CFF private-dict parser; sfnt hides it) | no | CJK stays 2.5–6.0 off | ~1 500 | 8–12 | high |
| Autohinter | 68-line experiment, rejected | worse at 2.0 | 5 000+ (skrifa: 8.4 k) | 25–35 | high |
| Grapheme clusters (UAX #29) | yes (119) | 766/766 conformance | 200 | 1 | low |
| Unicode data generator (dev-time fetch of UCD 17.0.0) | yes (137) | — | 250 | 1 | low |
| Line features: ellipsis, clip, colour spans, tracking, caret, measuring | ellipsis + clip (~40) | rows match | 500 | 3–4 | low–medium |
| Indic/USE/Khmer/Myanmar/jamo shapers | no | — | 5 000+ | 25+ | high |
| Variable fonts | no | — | 2 000 | 10–15 | high |

### D2.6 Recommended first scope

**Build:**
- the generic OpenType engine (GSUB 1–8, GPOS 1–9) with HarfBuzz-style normalisation and mark ordering;
- the default shaper (Latin, Greek, Cyrillic, CJK, precomposed Hangul), plus Hebrew and Arabic joining;
- per-cluster fallback through D4's resolver;
- the forked bidi;
- CBDT emoji with the swash-port scaling;
- the grapheme breaker for editing;
- unhinted outlines with whole-pixel advances and sRGB-space blending;
- the line features (ellipsis, clip, colour spans, tracking, caret, measuring).

*Inference:* about 6 000 lines of own code + the 1 400-line bidi fork + ~2 300 generated lines, **6–8 weeks** including the HarfBuzz-oracle and bidi conformance test infrastructure.

**Defer:** TrueType/CFF hinting, COLR v0/v1, sbix, complex-script shapers, vertical text, variable fonts.

**Expected quality against the hinted references:** identical shaping and placement for these scripts. Text will be visibly softer at scales 1.0–1.3 (the owner's output is 1.3). The owner should look at [`docs/evidence/text-corpus-compare@1.3.png`](../evidence/text-corpus-compare@1.3.png) before accepting that, because it is a decision about look, not correctness.

## D3 Keyboard

### D3.1 What a Wayland client actually needs

The compositor sends a fully resolved keymap (`wl_keyboard.keymap`, format `xkb_v1`, an fd), then `modifiers(depressed, latched, locked, group)` and `key(code, state)` events. A client never compiles RMLVO or `include` statements and never runs key actions. It mirrors the compositor's masks (`xkb_state_update_mask`) and asks what a key means. The work is therefore:

1. **Parse the serialised keymap** (text v1). Keysyms arrive as hex from libxkbcommon ≥ 1.x as niri sends them [local: `testdata/live-niri.xkb`], or as names from `xkbcli`, older libxkbcommon and other compositors [local: `testdata/xkbcli/`]. Both must work, including Unicode names like `U2019`.
2. **Derive what the text leaves implicit.** The serialiser omits `type=` where libxkbcommon can infer it, so the client must re-run `FindAutomaticType`: letters → ALPHABETIC, keypad → KEYPAD, FOUR_LEVEL_* variants [src: libxkbcommon `src/xkbcomp/symbols.c:1594`]. Virtual-modifier mappings (NumLock → Mod2, LevelThree → Mod5) and per-key repeat are not written either. They come from matching the compat `interpret` statements against each key's keysyms and `modifier_map`, most specific first [src: `src/xkbcomp/keymap.c` FindInterpForKey/ApplyInterpsToKey, `compat.c:989`]. A repeat flag defaults to *on* only through the default interpretation. Explicit vmod values (`Hyper=0x4000`, canonical virtual modifiers) appear in niri's own keymap but not in the re-serialised virtual-keyboard keymap [local], so both forms occur.
3. **State:** effective mods, group wrapping (per-key wrap, clamp or redirect), the type entry → level → keysyms, the Caps Lock transformation (`xkb_keysym_to_upper`, with ß → ẞ) and the Control transformation for text (Ctrl+G → BEL) [src: `src/state.c:1317–1550`].
4. **Consumed modifiers** in both modes (§D3.4), and **Compose** (§D3.5).
5. **Keysym data:** names ↔ values (2 638 names), keysym → Unicode, and case mapping. All are generated at development time from the installed `xkbcommon-keysyms.h` comments. Two deprecated keysyms (`leftanglebracket`, `rightanglebracket`) need an override, because libxkbcommon's conversion table disagrees with the header comment [test].

### D3.2 The prototype and its correctness

`research/go/xkb` (stdlib only; `xkb/` about 2 380 hand-written lines + 5 933 generated table lines; `kbd/` 318 lines: the keyboard layer with layout, state and repeat). Every claim below is a test.

| Check | Method | Result |
|---|---|---|
| Keysym conversions | every legacy keysym, special/function ranges, U+0000–U+2FFFF as keysyms, XF86 ranges: `to_utf32`, `to_upper`, `to_lower`; `utf32_to_keysym` for U+0000–U+2FFFF; all names round-trip | **0 mismatches** [test: `xkb/diff_keysym_test.go`] |
| Keymaps | 76 keymaps: the **live** keymap from the owner's niri (captured read-only by `cmd/keymapdump`), the two keymaps a nested niri sent, 59 RMLVO sets compiled and serialised by libxkbcommon (47 layouts and variants, among them de(neo), fr(bepo), ca(multix), ara, il, th, jp, kr; three multi-layout sets with group switching; caps/ctrl/numpad/lv3/lv5/compose/eurosign options), and 14 name-serialised by `xkbcli --format 1`. Each is compared over every keycode (8–709) × ~133 states: every real-modifier depressed mask in a bounded set × Caps/Num locked × each group, plus the masks libxkbcommon itself serialises after pressing each modifier key alone, tapped and in pairs. Compared: layout, level, keysyms, `one_sym`, UTF-8, consumed mods (XKB and GTK), active mods, repeat, modifier names and indices | **0 mismatches** in 7 645 807 key states; with `XKBDIFF_FULL=1` (all 256 depressed masks × 4 lock states × groups) **0 mismatches in 58 861 568** key states [test: `xkb/diff_keymap_test.go`] |
| Compose | system tables for en_US, C, de_DE, fr_FR, el_GR (1 903 seqs), pt_BR (5 150), ja_JP, fi_FI (5 154), km_KH: every sequence fed step by step, with a Shift keysym between steps, plus a cancelling key after the first step; parser edge cases (`include "%L"`, overriding prefixes and duplicates, escapes `\"` `\\` octal hex, invalid escapes, modifiers `!`/`~`/`None`, keysym-only results, `Uxxxx`/`0x` names, 11-key sequences, error lines) | **0 mismatches** [test: `xkb/diff_compose_test.go`] |
| Keyboard behaviour tests | layout, keyboard and repeat cases (Shift consumed by letters and Tab, Ctrl+G = BEL, Caps never a binding modifier, AltGr consumed so Ctrl+[ works on `de`, Num Lock keypad, second-layout base keysym for Ctrl+G on `us,ru`, Compose with a real Shift, repeat timeline, rate 0, keymap change/focus loss reset) | **all pass with `CGO_ENABLED=0`** [test: `kbd/kbd_test.go`] |
| feat/logis E2E scenarios in a nested niri | `cmd/e2eprobe` starts `niri -c <own config>` (the E2E harness's `NIRI_CONFIG`: us, `compose:menu`, repeat 300/30) as a window, refuses to type unless the socket's `SO_PEERCRED` pid is that child, maps an exclusive-keyboard overlay layer surface and types the feat/logis E2E scenario "compose, locks and the keypad type what the layout says" through `zwp_virtual_keyboard_v1`, as the E2E harness does: Menu o Shift+' ; Menu l g (harness XCOMPOSEFILE) ; Menu e e, BackSpace ; Caps a Caps a ; NumLock KP1 KP2 NumLock KP3 KP1 b ; then **holds x for 500 ms**. Run twice: harness config and harness config + `numlock` (the owner's setting) | replay through `kbd` gives `öλAa12b` at the expected checkpoints and `xxxxxxxx` (1 + **7 repeats** at 30/s after 300 ms) [test: `kbd/e2e_test.go`]; all 23 key presses of each trace give the same keysym, text, consumed mods and Compose state as libxkbcommon [test: `xkb/diff_e2e_test.go`] |

Observed in the nested niri [local]: the client first receives niri's own keymap. That keymap is byte-identical to the live one, since both are us + `compose:menu`. As soon as the virtual keyboard types, niri sends a **second keymap** (the injector's, recompiled; 35 601 bytes). A client must replace its keymap mid-session and reset Compose and repeat. With `numlock` in the config, the first `modifiers` event already carries `locked=0x10`.

### D3.3 Prior art: xkb-go (servitor-native-picker research)

The servitor research (lit `research/servitor-native-picker`, ticket ae7bb112) used `github.com/unxed/xkb-go` v0.1.9 and found that it "needed fixes". Confirmed in its source [src: `~/go/pkg/mod/github.com/unxed/xkb-go@v0.1.9`]:

- **Key types guessed by symbol count** (`parser.go:1747 guessKeyType`) instead of libxkbcommon's `FindAutomaticType`. Caps Lock does nothing on letters, Num Lock keypad digits come out as KP_Home…, and Shift turns keypad navigation into digits. On the live niri keymap: 96/2 717 keysym mismatches (servitor's measurement).
- **Compose `Feed` has no modifier guard** (`compose.go:71`), so a real Shift press inside Menu o " cancels the sequence. This is exactly the E2E scenario.
- **Virtual modifiers from a hard-coded name table** (`parser.go:713–728`: Alt→Mod1, NumLock→Mod2, Super/**Hyper→Mod4**, LevelThree→Mod5, LevelFive→Mod3) instead of computing them from interprets and `modifier_map`. It is wrong for the owner's live keymap, where Hyper is `0x4000` (its own canonical bit), and for any remapped keymap.
- `State.UpdateKey` is a TODO stub (`state.go:90`). There is **no consumed-modifier API**, `KeyGetUTF8` lacks the Control transformation and control characters, there is no Caps transformation for non-pair keys (é → É), Unicode keysym names (`U2019`) are unresolved, and one multi-group level-selection bug remains (servitor's findings).
- The servitor patches (automatic types, Compose modifiers, `#` in quotes) were deleted with that prototype; only the findings survive in the ticket comments.

**Is it a usable basis? No.** Under the dependency policy it cannot be a module dependency, and adopting it means owning a 20 558-line fork. Most of that is an RMLVO/include compiler and an X11 transport that a Wayland client never uses, structured around the defects above. The prototype here was written from libxkbcommon's semantics for the client-side subset alone. At about 2 400 lines it already passes the differential suite with zero mismatches, a level the servitor research reached only for the live keymap after patching.

### D3.4 Consumed modifiers, Caps Lock and key repeat

- **Consumed modifiers.** `XKB_CONSUMED_MODE_XKB` consumes every modifier in the key type's mask, minus the matching entry's `preserve`. `XKB_CONSUMED_MODE_GTK` consumes only modifiers whose type entries change the key's symbols, and of those only single-modifier entries or the active one [src: `state.c:1840 key_get_consumed`]. Both are implemented and match libxkbcommon on every keymap/state above [test]. On the owner's keymap the modes differ on 22 keys for each of Shift, Control and Alt held: BackSpace (TWO_LEVEL with identical levels), F1–F12 and keypad operators (the CTRL+ALT type) [test: `kbd/consumed_test.go`]. logis's shortcuts use XKB mode (`mod_index_is_consumed`), and its binding table accepts both outcomes (`BackSpace if plain || SHIFT`, ISO_Left_Tab with or without Shift). **Recommendation:** keep XKB mode and that binding table unchanged. Lock and Num Lock never take part in a binding.
- **Caps Lock** is applied as libxkbcommon does: the Caps transformation `keysym_to_upper` (Unicode simple upper case, ß → ẞ, legacy keysyms included) when Lock is active and not consumed. It affects keysyms and text, never shortcut matching (Ctrl+N with Caps still selects next) [test].
- **Key repeat** stays client-side: niri 26.04 offers `wl_seat` v9, sends `repeat_info` (live: 25/s after 600 ms; nested test config: 30/300) [local], and a v10 `wl_keyboard` "repeated" key state is not offered. Repeat (a deadline, at most 4 repeats per wake, skip ahead when late; X defaults 25/600 for `wl_keyboard` < v4; rate 0 disables) is a pure value type [test]. How it is armed depends on the event loop:
  - pure-Go transport (a reader goroutine + `select`): a `time.Timer` reset to the deadline, stopped when no key is held. There are no wake-ups while idle;
  - a single `poll()` loop (needed with libwayland's `prepare_read`): a `timerfd` (`unix.TimerfdCreate`/`TimerfdSettime` from golang.org/x/sys/unix; the stdlib `syscall` package has no timerfd wrapper), disarmed when idle.
  
  Key repeat must use the compositor's `key` timestamps only for ordering; deadlines use the local monotonic clock.

### D3.5 Compose

The file lookup is libxkbcommon's order: `XCOMPOSEFILE` → `$XDG_CONFIG_HOME/XCompose` (or `~/.config/XCompose`) → `~/.XCompose` → the locale's table via `compose.dir`. Locale aliases come from `locale.alias`, `C` maps to en_US.UTF-8, and an installed locale without a table falls back to en_US.UTF-8 [src: `src/compose/paths.c`, `table.c`]. Includes support `%L`, `%H` and `%S`, up to 5 deep. Modifiers in sequences are parsed and ignored, sequences hold at most 10 keysyms, and more than 10 bad lines fail the file. A later sequence overrides an equal one, a prefix, or a longer one, and an empty result string is a valid result. The owner has no `~/.XCompose`, `XCOMPOSEFILE` or XDG XCompose [local], so logis loads `/usr/share/X11/locale/en_US.UTF-8/Compose` (5 145 sequences). The E2E tests that use `XCOMPOSEFILE` and `HOME/.XCompose` are covered by the same lookup. The servitor-reported `%L` defect of xkb-go does not apply here [test: `include-L` table].

### D3.6 Measurements

`cmd/xkbbench`, each run one process, keymap = the live 35.6 KB keymap, Compose = en_US.UTF-8. One locked round (`results/d3-timing.txt`). **Load average 7.68 / 7.01 / 4.82 before and after.** 20 cold processes per implementation (first iteration = first-open cost), then 50 warm iterations.

| Measure | pure Go | cgo libxkbcommon 1.13.2 |
|---|---|---|
| Keymap parse+compile, cold (n=20) | median **1.98 ms**, p95 2.58 ms | median **1.19 ms**, p95 3.27 ms |
| Keymap parse+compile, warm (n=50) | median 1.53 ms, p95 3.27 ms | median 0.67 ms, p95 0.95 ms |
| Compose table load en_US (5 145 seqs), cold (n=20) | median **3.10 ms**, p95 5.27 ms | median **2.10 ms**, p95 5.92 ms |
| Compose load, warm (n=50) | median 3.32 ms, p95 5.90 ms | median 1.66 ms, p95 3.35 ms |
| Per key press (one_sym + UTF-8 + consumed), warm, n=50×1000 | median 181 ns, p95 588 ns | median 182 ns, p95 770 ns (incl. cgo call cost) |
| Heap | 696 KiB retained after GC for keymap + Compose (4.0 MiB allocated while parsing) | C heap, not measured; Go side 40 KiB |
| Binary | +266 KB stripped, static (1 597 600 → 1 863 840 bytes for a minimal program) | dynamically linked to `libxkbcommon.so.0` and libc; cgo toolchain needed |

Both are far inside the budgets. The keymap is parsed once per keymap event and Compose once per daemon. The ~1–2 ms parse can run while the Wayland handshake proceeds, and it is not on the warm-open path. The live-session capture itself (`keymapdump`, connect → keymap + repeat_info) took 0.37 ms [local, unlocked one-off].

### D3.7 Options compared

| | Pure-Go client XKB (this prototype) | cgo libxkbcommon | xkb-go fork |
|---|---|---|---|
| Correctness on owner's layout and E2E | equal to libxkbcommon on everything tested (0 mismatches) | the reference | 96/2 717 mismatches upstream on the live keymap; patched fork reached 0 on that keymap only |
| Dependency policy | stdlib only (x/sys/unix only if a timerfd loop is chosen) | allowed only if D1 needs cgo anyway; adds a dynamic library | third-party, not allowed as a module |
| Build | static, `CGO_ENABLED=0` | cgo, pkg-config, dynamic linking | — |
| Effort to production | *Inference:* 4–6 days. It needs hardening (fuzzing the parser, limits on hostile keymaps, errors), the text v2 format if a compositor ever sends it, and the differential suite in CI (cgo in tests only) | 1–2 days of bindings; libxkbcommon objects are **not thread-safe** (plain refcounts): one owner goroutine, no finalizers. The prototype's first finalizer-based wrapper crashed with an assertion in `xkb_keymap_unref` for this reason [local] | owning 20 k lines |
| Risks | semantic drift when libxkbcommon adds features (new keysyms: regenerate tables; text v2). Mitigation: keep the cgo differential suite in CI against the distro's libxkbcommon | cgo call overhead negligible (§D3.6); thread confinement; cross-builds | — |

**Recommendation D3:** pure Go, client-side scope only, as prototyped. Keep `ref/` as a test-only cgo oracle and run the differential suite (keysyms, a keymap corpus including the owner's live keymap, Compose tables, nested-niri traces) in CI wherever cgo and libxkbcommon exist. If D1 lands on cgo anyway, libxkbcommon via cgo is the cheaper path, but nothing in the keyboard forces cgo.

## D4 Fonts

The D4 sub-study (prototype `research/go/fonts/`, module `logis.research/go/fonts`, stdlib only; cgo only in `cgofc/` behind `//go:build cgo`) was run by a delegated sub-worker of this ticket. I re-ran its checks: `gofmt -l` empty, `go vet` clean with and without cgo, `CGO_ENABLED=0 go test -short ./...` green; full suite output in `results/tests-full.txt`.

### D4.1 What logis needs from fontconfig

logis does three things with fontconfig:
1. `FcInitLoadConfigAndFonts()`: the user's full configuration and caches.
2. For each theme face, a pattern with the family list in order and `FcWeightFromOpenType(weight)`, then `FcConfigSubstitute` + `FcDefaultSubstitute` + `FcFontMatch` → file and index.
3. For fallbacks, `FcFontSort(trim)` of the default family `sans-serif`, then, per character not yet seen, the first face in that order whose charset covers it.

The bundled themes ask for `["IBM Plex Mono","monospace"]@400/600`, `["Cinzel","serif"]@600` and `["VT323","Terminus","monospace"]@400`.

The owner's setup [local]:
- `~/.config/fontconfig/fonts.conf` aliases serif → FreeSerif, sans-serif → FreeSans, monospace → Source Code Pro. **None of those three are installed.**
- 51 `conf.d` files and 2 483 faces.

So every answer comes from fontconfig's stock rules:
- `IBM Plex Mono` → Noto Sans Mono, through `48-guessfamily`;
- `Cinzel` with serif → Noto Serif SemiBold;
- `VT323` → Noto Sans Mono;
- `sans-serif:lang=ja` → Noto Sans CJK **KR** (ttc index 1);
- Hebrew and Arabic fallback → DejaVu Sans;
- 👍 and 💻 → Noto Sans Symbols 2;
- 🔥, 💀, 🏽 and 🧑 → Noto Color Emoji.

A pure-Go resolver must reproduce exactly these subtleties.

### D4.2 The three options as prototyped

- **(a) Pure Go** (`fc/`, about 3 540 hand-written + 690 generated lines): a fonts.conf/conf.d parser (`include`, `dir`, `cachedir`, `alias`, `match` with `test`/`edit` and their qualifiers, modes and bindings) and a substitution engine. It also has fontconfig's `FcCompare`/`FcFontMatch`/`FcFontSort` with the language pass and trim, langsets and charsets. Fontconfig 2.18.3 sources were read for the semantics, not installed. Two ways to get the installed faces:
  - **(a2)** read fontconfig's own binary caches (`/var/cache/fontconfig`, `~/.cache/fontconfig`; format v12 since 2.18.2);
  - **(a1)** an own sfnt scanner (`scan/`, about 1 000 lines + fontconfig's orthography and generic-family data) with its own cache.
- **(b) Subprocess** (`fcsub/`, about 200 lines): the faces as parallel `fc-match --format=…` processes (one per theme face). The fallback order comes from one `fc-match -s sans-serif` with charsets, parsed once (a 78 KB listing).
- **(c) cgo** (`cgofc/`, about 140 lines): the three steps of D4.1 through libfontconfig, and the reference.

### D4.3 Correctness on the owner's real configuration

| Check | Result |
|---|---|
| Theme faces + extra queries (default, the four theme lists at their weights, serif/monospace/emoji, `sans-serif:lang=ja`, bare `Cinzel`/`IBM Plex Mono`, `DejaVu Sans Mono@700`, `Noto Sans@300`) | **14/14**: (a), (b) and (c) agree on file, index and family (`results/correctness-table.md`) |
| Fallback characters (機 万 기 ש U+05B8 م 🔥 ⚙ 💀 👍 🏽 🧑 💻 Æ Ş α ж ░ → …) | **20/20**; the trimmed sort is 182 faces in the same order in (a) and `fc-match -s` |
| Untrimmed `FcFontSort` for sans-serif, monospace, serif, emoji | identical at all **2 483** positions |
| Every installed family name + generics + uninstalled names (Arial, Helvetica…) × 3 weights | **1 152/1 152** equal to `fc-match` |
| (a1) own scanner vs fontconfig's cache | 2 483 faces identical on 18 fields (charset, langs, generic family, variable/named instances…), same order; 5 874/5 874 family queries and 26/26 first-open answers equal |
| Per-character `fc-match 'sans-serif:charset=X'` vs "first covering face in the sort" (what logis does) | **differ for 201 of 92 882 covered characters (0.2 %)**, e.g. U+0591 Noto Sans Hebrew (sort) vs Liberation Sans (per character), math letters Noto Sans Math vs DejaVu. None of the corpus characters differ. **A subprocess design must not query per character** (`results/perchar-vs-sort.txt`) |

### D4.4 First-open cost

"Ready" means every theme face and the default face are resolved and the fallback order is built, measured from `main()` of a fresh process. The machine was heavily shared during these rounds (load 6 → 29, swap full), and absolute values swing up to 5× between rounds. **The ratios inside an interleaved round are the reliable signal.**

**Interleaved round** (`results/bench-interleaved-1.txt`, **load 8.93 → 10.85**, N = 30 each), median / p95 (min):

| Option | Ready | Whole process | First fallback char once the order exists |
|---|---|---|---|
| (a2) pure Go on fontconfig's caches | 54.3 / 79.3 ms (36.3) | 59.4 / 84.2 ms | 機 7 µs, 🔥 22 µs |
| (c) cgo libfontconfig | 29.3 / 61.7 ms (21.9) | 34.2 / 66.3 ms | 21–72 µs (rounds vary) |
| (a1) own scanner, warm own cache | 76.9 / 130.8 ms (55.0) | — | — |
| (b) 7 `fc-match` in parallel + parsed `-s` | **26.4 / 38.2 ms** (16.4) | 28.4 / 41.0 ms | 5–17 µs (lookup in the parsed listing) |
| (b) sequential | 107.6 / 156.2 ms (82.5) | — | — |
| (b) one process per new character | faces 24.6 / 34.8 ms | — | **11.8–15.3 ms** median each (and wrong in 0.2 % of characters) |

Other rounds:
- The **quietest round** (load 9.43 → 8.76, N = 30) gave (a2) 37.8 / 42.5 ms, (c) 25.3 / 31.1 ms and (a1) 59.5 / 71.9 ms. The (a2) breakdown: config 4.1, font set 17.4, faces 9.3, sort 6.1 ms. The (c) breakdown: init 7.7, faces 12.7, sort 3.5 ms.
- **Contended rounds:** at load 19 → 17 (N = 40), pure 184 / 238, cgo 129 / 197, parallel (b) 99 / 127 ms. In the final round at **load 13.9 → 29.4** (N = 30), pure 77 / 815, cgo 52 / 527, parallel (b) 32 / 550, sequential (b) 141 / 1 794 ms.
- **(a1) cold scan:** 2 399 files and 2 483 faces, page cache warm, took 1 300 / 1 486 ms (N = 5, load 9.38 → 8.70). Writing the own cache took 1.7–2.0 s and produced 3.2 MB.

Resolving fonts on a thread that overlaps the Wayland and GPU start-up keeps this inside the 150 ms first-open budget. Any of (a2), (b)-parallel or (c) fits that way: once per daemon start, off the warm-open path. Sequential (b) and (a1) without a cache do not fit comfortably under load.

**Memory** (N = 5, load 6.09): (c) RSS 15.4 MB after init (4.1 MB anon + 7.9 MB mmapped cache). (a2) RSS 44.4 MB, of which the Go heap is 28.3 MB including garbage; decoding only what matching needs gives 12.8 MB live after GC, and a compact layout would reach *~5 MB (inference)*. (a1) cache 35.6 MB. Parallel (b) 7.6 MB, and the processes exit. The 64 MB RSS budget favours (b) and (c).

**Binary** (stripped, vs a 1.51 MB hello world): (a) 2.24 MB (+0.72) static; (b) 2.02 MB (+0.51) static; (c) 1.57 MB (+0.06) but **forced dynamic**: libfontconfig, libfreetype, libexpat, zlib, bz2, png, brotli. Arch ships no `libfontconfig.a`, so a static cgo build is impossible [local].

### D4.5 Effort and risk

| Option | Production effort (*inference*) | Risks |
|---|---|---|
| (a2) pure Go on fontconfig's caches | 4–5 k LOC, 8–10 days | **Cache format churn:** v9 (2.15–2.17.1) → v11 (2.18.0/2.18.1, May–Jun 2026) → v12 (2.18.2, Jul 2026). **Matching semantics changed in 2.18:** a generic-family object and priority, scan rules generated from aliases, `xsi:nil`. A port tracks fontconfig forever. Arch's build validates the cache mtime to seconds only, which must be copied. A directory without a valid cache means rescanning. The header's `fc_version` allows detecting drift and falling back |
| (a1) own scanner | +2–3 k LOC, +1–2 weeks | 1.3–2 s first scan and its own invalidation; the same semantic drift as (a2) |
| (b) `fc-match` subprocess | ~400 LOC, 1–2 days | needs fontconfig's CLI (Arch's `fontconfig` package ships it; Debian splits it into `fontconfig` vs `libfontconfig1`, so the PKGBUILD/packaging must depend on it); timeouts, exit codes and parse errors; **pattern escaping** (an unescaped `fc-match sans-serif` actually asks for family "sans", which works only because fonts.conf maps sans → sans-serif); 10–50 ms of CPU per process on small machines; never use per-character queries. It never drifts: it *is* the system's fontconfig |
| (c) cgo libfontconfig | ~250 LOC, ~1 day | cgo toolchain, cross-builds, a dynamic link to 6+ libraries; behaves like every desktop app by construction; fastest and smallest |

## Risks (all areas)

| Risk | Area | Likelihood | Mitigation |
|---|---|---|---|
| Unhinted text looks softer than hinted text at scale 1.0–1.3 | D2 | certain (measured) | Owner decides from `docs/evidence/text-corpus-compare@*.png`. Hinting is a 30–40+ day follow-up; the snapping stand-in was measured and rejected |
| Shaping long tail (other scripts, real fonts with context/chained lookups) diverges from HarfBuzz | D2 | likely over time | Keep a HarfBuzz-oracle corpus in the test suite (dev-time oracle, not a dependency); generic GSUB/GPOS engine, not corpus-specific |
| x/text/unicode/bidi bracket pairing bug and missing levels | D2 | certain (found) | Ship the fork; report upstream; drop the fork when fixed |
| sfnt gaps: no variable fonts, no cmap 14, no EBDT bitmap fonts (a Terminus `.otb` would not render), CFF private dict hidden | D2 | possible | Defer; fall back to the next fontconfig face when a face is unusable |
| Unhinted renders cannot pass goldens recorded from hinted text | D2 | certain | New goldens, reviewed once against the hinted references |
| Client XKB drifts from libxkbcommon (new keysyms, text v2 keymaps, wl_keyboard v10 repeated keys) | D3 | possible | Regenerate keysym tables; the cgo differential suite in CI; bind wl_keyboard ≤ v9 or handle the repeated state |
| Hostile or malformed keymaps from a buggy compositor | D3 | low | Fuzz the parser; size and depth limits; on failure keep the last good keymap |
| libxkbcommon/fontconfig through cgo are not thread-safe for shared objects | D3/D4 | real (crash observed) | Confine to one goroutine, explicit unref, no finalizers |
| `fc-match` missing or slow; per-character use gives wrong answers | D4 | low / certain if misused | Packaging dependency; parallel face queries + one parsed `-s` sort; timeouts; clear error ("no font answers the rite") |
| Fontconfig internals churn (cache v9→v11→v12, 2.18 matching changes) | D4 (a) | certain | Do not port fontconfig; if ever needed, gate on the cache header's `fc_version` and fall back to (b) |
| Measurements taken at load 6–29 | all | certain | Ratios within interleaved rounds are the signal; rerun on a quiet machine before setting budgets |

## Recommendations per decision

- **D2 text:** build the generic OpenType shaping subset on x/image/font/sfnt + x/image/vector, the forked x/text bidi, Arabic joining, CBDT emoji with the swash-port filter, the UAX #29 grapheme breaker and the line features. Ship **unhinted**, with whole-pixel advances and sRGB blending: *about 6 000 own lines + 1 400 fork + ~2 300 generated; 6–8 weeks*. Defer hinting (TrueType + CFF: 30–40+ days), COLR/sbix, complex-script shapers and variable fonts. **Owner decision needed:** whether softer unhinted text is acceptable, judged from the evidence images.
- **D3 keyboard:** **pure-Go client-side XKB** (keymap text v1, state from masks, consumed modifiers in XKB mode, Compose with libxkbcommon's lookup, client-side repeat on a stopped `time.Timer`, or on a timerfd if the event loop is `poll()`-based). About 2 400 lines + generated tables, *4–6 days* to harden. cgo libxkbcommon only as the test oracle in CI. Do not fork xkb-go.
- **D4 fonts:** **`fc-match` subprocesses from the daemon** (faces in parallel, one parsed `fc-match -s` for the fallback order, never per character) while the binary is `CGO_ENABLED=0`, at *~400 LOC, 1–2 days*. If D1 chooses cgo for rendering, use **cgo libfontconfig** instead (*~250 LOC, 1 day*). Do not port fontconfig, although it was shown to be feasible and exact.
- **Interplay with D1/D5:** nothing in text, fonts or keyboard *requires* cgo. If D1 requires cgo anyway, the D4 answer switches to cgo libfontconfig, and D3 could switch to libxkbcommon too at no correctness gain.

## Reproduce

Each prototype is its own module; run from its directory.

```sh
# D3 (research/go/xkb)
CGO_ENABLED=0 go test ./kbd/                       # keyboard behaviour tests + nested-niri trace replay
go test ./xkb/                                     # cgo differential vs libxkbcommon (XKBDIFF_FULL=1 for all masks)
WAYLAND_DISPLAY=wayland-1 go run ./cmd/keymapdump  # read-only keymap capture
WAYLAND_DISPLAY=wayland-1 go run ./cmd/e2eprobe    # nested niri only; refuses any other compositor
# D2 (research/go/text)
CGO_ENABLED=0 go test ./... && go run ./cmd/corpus # renders out/ and docs/evidence/
# D4 (research/go/fonts)
CGO_ENABLED=0 go test -short ./... && go test ./cgofc
go run ./cmd/fontbench -table                      # correctness table vs fc-match and cgo
```

Timing rounds: build first, then one command per round under
`flock -w 3600 /home/nerdwave/.claude/jobs/88752f6d/tmp/measure.lock sh -c 'cat /proc/loadavg; <cmd>; cat /proc/loadavg'`.
The exact commands are in `research/go/xkb/results/measure-d3.sh`, `research/go/text/results/commands.txt` and `research/go/fonts/README.md`.

Golang.org/x packages used and why:
- **D2:** x/image/font/sfnt (font parsing, cmap, advances, outlines), x/image/math/fixed (its number type), x/image/vector (coverage rasterisation), x/image/font (hinting constants in probes), x/image/draw (only for the filter comparison), x/image/font/opentype (only in tests, to show its Kern bug), x/text/unicode/bidi (classes; forked core), x/text/unicode/norm (combining classes), and x/sys indirectly via vector's CPU detection.
- **D3, D4:** none; the standard library sufficed. golang.org/x/sys/unix would only be needed for a timerfd-based repeat in a `poll()` event loop.

Development-time tools outside the modules, none of them shipped:
- the cgo libxkbcommon and libfontconfig references;
- the system libharfbuzz through Python ctypes as the shaping oracle;
- Unicode 17.0.0 data files fetched from unicode.org by `cmd/genucd`;
- `xkbcli` for name-serialised keymap fixtures;
- libxkbcommon 1.13.2 and fontconfig 2.18.3 sources, read-only, for semantics.
