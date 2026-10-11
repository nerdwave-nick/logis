# Rust on wgpu with logis's own renderer

Status: accepted (2026-10-11, owner decision on ddba629b, /clarify Q1 and Q3).
Evidence: [`docs/research/stack.md`](../research/stack.md) (verdict, §2 measurements,
§4 language evaluation, §5 foundations, §7 risks) and
[`docs/research/landscape.md`](../research/landscape.md).

logis must look like a cogitator of the Machine God (animated glow, scanlines, grain,
gradients, blur behind) and still open instantly. These are per-pixel, continuously
animated effects, so they belong on the GPU (stack.md §1). Forking an existing
launcher was ruled out: logis is clean-room. We decided to build logis in **Rust**
on these foundations:

- **Wayland:** smithay-client-toolkit 0.21 on wayland-rs with the **system** backend
  (libwayland-client), plus wayland-protocols. The system backend is required
  because GPU drivers need a real `wl_display*`; wgpu needed it in the research
  (stack.md §4, §5.2).
- **Rendering:** **wgpu**, Vulkan first, then wgpu's GL backend, then Mesa
  lavapipe/llvmpipe where no usable GPU exists. **No separate CPU renderer.**
- **logis's own small renderer** on top: SDF primitives (rounded rectangles,
  borders, glows, shadows, gradients) in WGSL, glyph quads from a glyphon atlas, an
  icon atlas (resvg rasterises SVGs once per size), and an optional post-processing
  pass (bloom, scanlines, grain, curvature, chromatic aberration). A launcher's UI
  vocabulary is small (input line, list, rows), so a retained scene with a tiny
  layout engine suffices (stack.md §5.3).
- **Text:** cosmic-text (rustybuzz shaping, swash rasterisation with hinting, bidi)
  through glyphon, with families and fallbacks resolved by libfontconfig
  in-process. `FontSystem::new()`, a full font scan costing 140 ms per launch, is
  never used (stack.md §2.1, §5.4).
- **Input:** libxkbcommon through sctk.

The research spike (`research/proto-rust/src/bin/spike.rs`, `panel.wgsl`) drew the
glowing brass-to-crimson panel with scanlines, grain, flicker, a travelling
highlight, GPU text and compositor blur for **1.1 ms of GPU wait per frame** (2.1 ms
fullscreen at 1920×1200) on Intel Arc / Mesa / niri (stack.md §2.3,
`research/evidence/spike.png`). Every layer needed exists as a maintained Rust
crate; in Go each layer is a cgo binding to write, a young dependency, or missing
(stack.md §4). An independent review on a neutral briefing reached the same stack
(stack.md §8).

## Considered Options

- **Fork fuzzel** (C, MIT) — ruled out by the owner: clean-room only, no fork.
- **Go with cgo** (libwayland, wgpu-native, libxkbcommon, fontconfig, resvg
  bindings) — feasible, but roughly 2–3× the foundation work with more
  cross-language debugging (inference, stack.md §4.1). Declined.
- **Go + gotk4 + GTK4 + gtk4-layer-shell** — gives text, IME, accessibility and
  widgets, but custom shader effects need a `GtkGLArea` with our own GL renderer
  anyway and GTK starts in 61–79 ms (stack.md §4.1). Declined: owning the look
  matters more.
- **C, Zig, C++/Qt Quick** — slow feature velocity and no memory safety (C), a
  pre-1.0 language (Zig), or a heavy runtime with a 152 ms cold start and a theming
  model owned by Qt (Qt Quick) (stack.md §4.2).
- **vello** — 72 ms cold start, 968 ms first shader compile, pre-1.0 churn; more
  vector power than a launcher needs (stack.md §5.3).
- **iced / libcosmic, GPUI** — font loading at startup, opinionated widget and style
  models, churn (stack.md §5.3, landscape.md).
- **CPU-only rendering (tiny-skia)** — fine for static themes, but animated
  fullscreen effects at HiDPI are out of reach (inference, stack.md §5.3). The
  owner chose GPU only; lavapipe/llvmpipe covers machines without a GPU.
- **Raw Vulkan (ash)** — half wgpu's device-init time, far more code; irrelevant in a
  resident daemon. Kept as the escape hatch if wgpu churn hurts (stack.md §7).

## Consequences

- logis owns its UI infrastructure: cursor and grapheme editing, clipping and
  ellipsis, fallback fonts, and later IME preedit (text-input-v3) and accessibility
  (AccessKit). Both of the latter are out of v1 (0d0e086d), but the text-input and
  widget model must allow adding them without a rewrite (stack.md §7).
- wgpu majors arrive about quarterly and glyphon pins them: pin, upgrade
  deliberately, keep the renderer surface small (stack.md §7).
- Defects found in the spike are requirements now (stack.md §7): wgpu surfaces drop
  before the `wl_surface` they reference, encoded in types; the present mode is
  negotiated (FIFO is the only guaranteed one); the frame loop never blocks on GPU
  completion; a real configure/scale state machine handles resizes, scale and output
  changes; glyphon's colour mode matches the swapchain format.
- A GPU path costs 12–70 ms at cold start (stack.md §1, §2.1), which is why logis keeps
  it resident in a daemon (ADR 0002).
- libwayland-client is a build and runtime dependency of the binary.
