# Go with a small cgo set and logis's own GPU renderer

Status: accepted (2026-10-13, owner decisions on 9189f099 — D1, D3 and D4 as
proposed, D2 and D5 revised by the owner the same day; the presentation path and
cold-start measurement clarified that morning). The GPU-only look, the own
renderer and the clean-room rule go back to the owner's decision on ddba629b
(2026-10-11). Shared specification: lit project `feat/logis-go`, §4, §5 and §8.
Evidence: [`docs/research/rendering.md`](../research/rendering.md) (D1, D5)
and [`docs/research/text-input.md`](../research/text-input.md) (D2, D3, D4),
with the screenshots in [`docs/evidence/`](../evidence/), and
[`docs/research/landscape.md`](../research/landscape.md). The prototypes
they measured stay on the local branch `research/go-foundations`
(`research/go/`); they may be mined, never imported.

logis must look like a cogitator of the Machine God (animated glow, scanlines,
grain, gradients, blur behind) and still open instantly. These are per-pixel,
continuously animated effects, so they belong on the GPU. Forking an existing
launcher is ruled out: logis is clean-room. logis is written in **Go**, the
owner's language, from the **Go standard library and `golang.org/x` modules
only**; every other layer is hand-rolled (spec §4, §5). Each `golang.org/x`
package is justified by the ticket that introduces it. A few C libraries the
system already ships do the work no reasonable amount of Go replaces. We
decided:

- **The look is GPU-only, drawn by logis's own small retained renderer.** SDF
  primitives (rounded rectangles, borders, glows, shadows, gradients), glyph
  quads from a glyph atlas, an icon atlas (each icon rasterised once per size)
  and a post-processing pass. A launcher's UI vocabulary is small (input line,
  list, rows), so a retained scene with a tiny layout engine suffices. **There
  is no CPU renderer.**
- **D1 Rendering: GPU through cgo to EGL + OpenGL ES 3.** Own thin bindings and
  own GLSL ES shaders, compiled by the driver at start, presented into the layer
  surface through the **EGL Wayland platform** (`wl_egl_window` +
  `eglSwapBuffers`). Measured: warm open 2.6–3.7 ms, keystroke → commit 1.0 ms
  with effects static, 0.07–0.10 ms CPU per animated frame, idle 0 % CPU and 0
  wake-ups, RSS 55–61 MB, 2.25 MB stripped binary (rendering.md §3).
- **D2 Text: own shaping, hinted glyphs from the system FreeType.** A generic
  OpenType engine (GSUB 1–8, GPOS 1–9) on `x/image/font/sfnt`, HarfBuzz-style
  normalisation, default shaper plus Hebrew and Arabic joining, a fork of
  `x/text` bidi, an own UAX #29 grapheme breaker. libfreetype loads, hints and
  rasterises glyphs (TrueType bytecode, CFF hinter, autohinter for unhinted
  fonts) and CBDT emoji via `FT_LOAD_COLOR`. Deferred: COLR, sbix,
  complex-script shapers, vertical text, variable fonts.
- **D3 Keyboard: own pure-Go client-side XKB.** Keymap text v1 parser, state from
  the compositor's masks, consumed modifiers, Compose in libxkbcommon's lookup
  order, client-side repeat; the keymap can be replaced mid-session. 0 mismatches
  against libxkbcommon 1.13.2 over 76 keymaps and 9 Compose tables
  (text-input.md D3). libxkbcommon is a test-only oracle.
- **D4 Fonts: cgo libfontconfig, in-process**: one `FcFontMatch` per theme face,
  one `FcFontSort` for fallback, never a full font scan and never a query per
  character.
- **D5 Wayland: libwayland-client through cgo.** Protocol C code from
  `wayland-scanner`, Go-side wrappers generated from the same XML files by an own
  generator, dispatch on one locked goroutine (`wl_display_prepare_read` /
  `read_events` in a poll loop).

The shipped binary therefore links **exactly** libwayland-client, libwayland-egl,
libEGL, libGLESv2, libfontconfig and libfreetype (with libffi under libwayland),
and nothing else: no libvulkan, libxkbcommon or libharfbuzz. Each C object
(`wl_display` and its proxies, the EGL/GL context, the fontconfig config,
`FT_Library`/`FT_Face`) is owned by one goroutine pinned with
`runtime.LockOSThread` and released explicitly, never through finalizers.

## Considered Options

- **Fork an existing launcher** (fuzzel: C, MIT) — ruled out by the owner:
  clean-room only, no fork.
- **A pure-Go CPU renderer** — reached the forge look only through effect-specific
  caching, at the edge of the budgets, and could not do bloom or curvature (warm
  open 268–362 ms with them; rendering.md §3–§4). Rejected: the look is GPU-only.
- **Own offscreen buffers exported as dmabufs** (`zwp_linux_dmabuf_v1`) — answered
  keys during animation in 0.11 ms against 16.7 ms for Mesa's `eglSwapBuffers`,
  but relies on Mesa-specific EGL extensions, implicit sync and own tiled
  allocation. Kept as a possible later optimisation.
- **An own pure-Go Wayland wire protocol** (decided first, then revised) — the
  owner preferred the system library; the costs are accepted below.
- **Shipping unhinted text** (decided first, then revised) — visibly softer than
  hinted text at the owner's scale 1.3
  ([`docs/evidence/text-corpus-compare@1.3.png`](../evidence/text-corpus-compare@1.3.png));
  own hinting was estimated at 30–40+ days. FreeType hints instead.
- **libxkbcommon or libharfbuzz in the binary** — not needed: the pure-Go XKB
  matched libxkbcommon exactly, and the scripts logis shapes are within the own
  engine. Both stay test-only oracles.
- **An own fontconfig port** — rejected; cgo is in the build anyway (D1).

## Consequences

- The build needs a C toolchain, pkg-config and the six libraries' development
  files; the binary is dynamically linked. `wayland-scanner` runs on `go generate`;
  its output is committed so a plain `go build ./cmd/logis` works. A test holds
  the binary's `DT_NEEDED` set to exactly the six libraries.
- C callbacks into Go, and cgo glue per protocol; libffi arrives with
  libwayland-client. The research's first open with libwayland (57 ms in one round,
  317 ms in another at equal load) is unexplained; the owner benchmarks cold
  start separately.
- logis owns its UI infrastructure: cursor and grapheme editing, clipping and
  ellipsis, fallback fonts, and later IME preedit (text-input-v3) and
  accessibility; the text-input and widget model must allow adding them.
- Known risks carried into implementation (spec §8): Mesa's `eglSwapBuffers`
  blocked ~16.7 ms when a key arrived while effects animated; the GPU stayed at
  its floor clock, so heavy frames need cheaper shaders; untested on NVIDIA, AMD
  and llvmpipe; the Go runtime wakes 2 400–4 400 times/s while effects animate;
  all research numbers were taken at load 5–29.
- A GPU path is expensive at cold start, which is why logis keeps it resident in
  a daemon (ADR 0001).
