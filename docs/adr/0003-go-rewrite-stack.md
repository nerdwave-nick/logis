# Go with a small cgo set, on a branch of its own

Status: accepted (2026-10-13, owner decisions on 9189f099 — D1, D3, D4 and D6 as
proposed, D2 and D5 revised by the owner the same day; the presentation path and
cold-start measurement clarified that morning). Shared specification: lit project
`feat/logis-go`, §4, §5 and §8.
Evidence: [`docs/research/go-rendering.md`](../research/go-rendering.md) (D1, D5)
and [`docs/research/go-text-input.md`](../research/go-text-input.md) (D2, D3, D4),
with the screenshots in [`docs/evidence/go/`](../evidence/go/). The prototypes
they measured stay on the local branch `research/go-foundations`
(`research/go/`); they may be mined, never imported.

The owner wants logis rewritten in **Go**, behaviour for behaviour (2026-10-12):
the Rust v1 lock file holds 204 packages, and Go is the owner's language. The
rewrite is built from the **Go standard library and `golang.org/x` modules only**;
every other layer is hand-rolled (spec §4, §5). Each `golang.org/x` package is
justified by the ticket that introduces it. A few C libraries the system already
ships do the work no reasonable amount of Go replaces. We decided:

- **D1 Rendering: GPU through cgo to EGL + OpenGL ES 3.** Own thin bindings and
  GLSL ES ports of `sdf.wgsl` and `post.wgsl`, compiled by the driver at start,
  presented into the layer surface through the **EGL Wayland platform**
  (`wl_egl_window` + `eglSwapBuffers`). **No CPU renderer**: ADR 0001's rule stands.
  Measured: warm open 2.6–3.7 ms, keystroke → commit 1.0 ms with effects static,
  0.07–0.10 ms CPU per animated frame, idle 0 % CPU and 0 wake-ups, RSS 55–61 MB,
  2.25 MB stripped binary (go-rendering.md §3).
- **D2 Text: own shaping, hinted glyphs from the system FreeType.** A generic
  OpenType engine (GSUB 1–8, GPOS 1–9) on `x/image/font/sfnt`, HarfBuzz-style
  normalisation, default shaper plus Hebrew and Arabic joining, a fork of
  `x/text` bidi, an own UAX #29 grapheme breaker. libfreetype loads, hints and
  rasterises glyphs (TrueType bytecode, CFF hinter, autohinter for unhinted
  fonts: what swash/skrifa emulate) and CBDT emoji via `FT_LOAD_COLOR`. The load
  target is chosen against the Rust goldens. Deferred: COLR, sbix, complex-script
  shapers, vertical text, variable fonts.
- **D3 Keyboard: own pure-Go client-side XKB.** Keymap text v1 parser, state from
  the compositor's masks, consumed modifiers, Compose in libxkbcommon's lookup
  order, client-side repeat; the keymap can be replaced mid-session. 0 mismatches
  against libxkbcommon 1.13.2 over 76 keymaps and 9 Compose tables
  (go-text-input.md D3). libxkbcommon is a test-only oracle.
- **D4 Fonts: cgo libfontconfig**, as Rust `src/text/fc.rs` does it: one
  `FcFontMatch` per theme face, one `FcFontSort` for fallback, never a query per
  character.
- **D5 Wayland: libwayland-client through cgo.** Protocol C code from
  `wayland-scanner`, Go-side wrappers generated from the same XML files by an own
  generator, dispatch on one locked goroutine (`wl_display_prepare_read` /
  `read_events` in a poll loop).
- **D6 Branch and switch-over.** The rewrite lives on the long-lived branch `go`,
  branched from `main` at 2ffdd02; the Rust code there stays the untouched
  reference. CI runs for both languages, each workflow only for its own paths.
  Go replaces Rust on `main` in **one merge**, once every E2E scenario carried over
  from `feat/logis` passes and the spec §3 budgets hold under the latency harness
  on a quiet machine. Rust stays in `main`'s history; there is no Rust tag.

The shipped binary therefore links **exactly** libwayland-client, libwayland-egl,
libEGL, libGLESv2, libfontconfig and libfreetype (with libffi under libwayland),
and nothing else: no libvulkan, libxkbcommon or libharfbuzz. Each C object
(`wl_display` and its proxies, the EGL/GL context, the fontconfig config,
`FT_Library`/`FT_Face`) is owned by one goroutine pinned with
`runtime.LockOSThread` and released explicitly, never through finalizers.

## Considered Options

- **A pure-Go CPU renderer** — reached the forge look only through effect-specific
  caching, at the edge of the budgets, and could not do bloom or curvature (warm
  open 268–362 ms with them; go-rendering.md §3–§4). Rejected; ADR 0001 stands.
- **Own offscreen buffers exported as dmabufs** (`zwp_linux_dmabuf_v1`) — answered
  keys during animation in 0.11 ms against 16.7 ms for Mesa's `eglSwapBuffers`,
  but relies on Mesa-specific EGL extensions, implicit sync and own tiled
  allocation. Kept as a possible later optimisation, not v1.
- **An own pure-Go Wayland wire protocol** (decided first, then revised) — the
  owner preferred the system library; the costs are accepted below.
- **Shipping unhinted text** (decided first, then revised) — visibly softer than
  Rust v1 at the owner's scale 1.3 (`docs/evidence/go/text-corpus-compare@1.3.png`);
  own hinting was estimated at 30–40+ days. FreeType hints instead.
- **libxkbcommon or libharfbuzz in the binary** — not needed: the pure-Go XKB
  matched libxkbcommon exactly, and the scripts v1 shapes are within the own
  engine. Both stay test-only oracles.
- **An own fontconfig port** — rejected; cgo is in the build anyway (D1).
- **A separate repository, or replacing Rust piecemeal on `main`** — rejected:
  `main` keeps a working Rust logis until the Go build reaches parity.

## Consequences

- The build needs a C toolchain, pkg-config and the six libraries' development
  files; the binary is dynamically linked. `wayland-scanner` runs on `go generate`;
  its output is committed so a plain `go build ./cmd/logis` works. A test holds
  the binary's `DT_NEEDED` set to exactly the six libraries.
- C callbacks into Go, and cgo glue per protocol; libffi arrives with
  libwayland-client. The research's first open with libwayland (57 ms in one round,
  317 ms in another at equal load) is unexplained; the owner benchmarks cold
  start separately.
- Known risks carried into implementation (spec §8): Mesa's `eglSwapBuffers`
  blocked ~16.7 ms when a key arrived while effects animated; the GPU stayed at
  its floor clock, so heavy frames need cheaper shaders; untested on NVIDIA, AMD
  and llvmpipe; the Go runtime wakes 2 400–4 400 times/s while effects animate;
  FreeType against skrifa hinting is unmeasured against the Rust goldens; all
  research numbers were taken at load 5–29.
- Two CI workflows run side by side until the switch-over; shared fixtures,
  fonts, goldens, themes and schemas trigger both.
- ADR 0002 (resident daemon and thin client) holds unchanged for the Go build.
  ADR 0001's Rust and wgpu foundations are replaced on `go`, and on `main` at the
  switch-over; its "no CPU renderer" rule is carried over.
