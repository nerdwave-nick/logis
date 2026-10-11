# Research: tech stack and foundations for logis

Date: 2026-10-11. Machine: Arch Linux, niri 26.04, Intel Arc (Meteor Lake) iGPU, Mesa 26.2.3, wayland 1.26.0, libxkbcommon 1.13.2. Toolchains: Rust 1.91.1, Go 1.27.1, gcc 15. eDP-1 runs at 60 Hz, scale 1.3.
Tracker: lit project `feat/logis`, issue "Research: tech stack and foundations for logis". This research feeds the decision ticket "Decide: implementation route …".
Companion: [`landscape.md`](landscape.md), a cited survey of 26 launchers and 6 terminals.
Evidence code: `research/` (benchmarks in `research/bench/`, Rust prototypes and the spike in `research/proto-rust/`, screenshots in `research/evidence/`). It is disposable and not a foundation.

Source tags: **[m]** = measured here (method in §2). **[src]** = read in source at the stated version. **[land]** = `landscape.md` (cited there). *Inference* = my judgement, not a verified fact.

## Question

Which language and stack should logis be built on, and on what foundations? It must:
- be themable, with a polished grimdark-40k look;
- be snappy;
- offer a plain dmenu mode, an app launcher, and first-class servitor integration;
- be modern, using current Wayland features and proper rendering;
- be built clean-room, with no fork.

Go is preferred, but any language that is significantly better should be chosen.

## Verdict

**Rust**, on these foundations:

1. **Process model: a resident daemon plus a thin client** (`logis` opens the daemon's window over a Unix socket; dmenu mode streams stdin to it). Optionally socket-activated, with a one-shot fallback when no daemon runs.
2. **Wayland: smithay-client-toolkit 0.21 on wayland-rs** with its `system` backend (libwayland-client, which GPU presentation requires), plus `wayland-protocols` for everything modern.
3. **Rendering: wgpu (Vulkan primary, GL fallback)** driving logis's **own small renderer**: signed-distance-field (SDF) shapes and effects in WGSL, glyph atlas text, image atlas for icons, and an optional post-processing pass. A UI toolkit is not recommended (§5.3).
4. **Text: cosmic-text (shaping, swash rasterisation with TrueType hinting) via glyphon's GPU atlas.** Fonts resolved through **libfontconfig** in-process, never a full font scan.
5. **Input: libxkbcommon** through sctk's keyboard integration.
6. **Integrations: a process-based, streaming JSON-lines provider protocol.** servitor is a first-class built-in provider over that protocol; it is not linked into logis.

The deciding evidence:
- **The look.** One WGSL shader gives a glowing brass-to-crimson SDF panel with scanlines, grain, flicker and a travelling highlight, plus GPU text and compositor blur. It costs **1.1 ms of GPU per frame**, or **2.1 ms fullscreen at 1920×1200** [m]. Screenshots: `research/evidence/spike.png`, `spike-blur.png`.
- **Snappy.** The resident daemon opens in **6.5–8.9 ms** from client request to committed frame, against **22–38 ms** for every one-shot launcher measured, fuzzel included. It holds **27 MB RSS** [m].
- **The ecosystem.** Every layer needed exists, maintained, in Rust. In Go, each layer is a cgo binding to write, a young dependency, or missing (§4).

Go remains *feasible* (§4.1). The difference is the amount of foundation work and risk, not possibility. Your Go preference is a legitimate counterweight that this research cannot settle for you.

## 1. Requirements → technical consequences

| Requirement | Consequence |
|---|---|
| Looks amazing, grimdark | Glow/bloom, scanlines, grain, flicker, gradients, gothic frames, animated transitions, blur behind. These are per-pixel effects, so a **GPU shader path** is the natural fit. A CPU path can do static versions; animated fullscreen effects at HiDPI exceed CPU budgets (*inference*). |
| Snappy | Fast open is the goal. A GPU path costs 12–70 ms at cold start (§2), so the GPU must stay resident: **daemon + thin client**. |
| dmenu | A thin client must start in milliseconds, stream stdin, honour exit codes, and print the choice. Being stdin/stdout-compatible gives every script and servitor a path. |
| App launcher | `.desktop` parsing, icon-theme lookup, **SVG** icons, frecency, launching with `xdg-activation` tokens into `systemd` scopes. A warm index in a daemon makes it instant. |
| servitor first-class | Structured items (ids, columns, standing glyphs), multi-stage flows (rite → aspect → inscriptions), asynchronous loading (§6). |
| Modern Wayland | layer-shell, fractional-scale + viewporter, cursor-shape, xdg-activation, ext-background-effect (blur), presentation-time, single-pixel-buffer, fifo/commit-timing, color-management later. |
| Clean-room | No fork. Libraries are fine; another launcher's code is not. |

## 2. Measurements [m]

Method: `research/bench/launch.py` records CLOCK_MONOTONIC just before `exec` (`T0_NS`); programs print milestones relative to it, so exec and dynamic linking are included. Toolkits were timed from `WAYLAND_DEBUG` timestamps (`wldebug_timing.py`, exec → first buffer attach+commit). The surface is 832×351 px unless noted. Values are medians.

Caveats [2nd]:
- The two methods are not perfectly comparable. `launch.py` uses monotonic time; `wldebug_timing.py` uses wall-clock and libwayland's timestamps and does not correlate surface ids.
- "Committed" is not presentation and not keyboard readiness.
- First-ever runs and tails differ from medians. For example, one blur-enabled spike run took 147.6 ms to first commit (`research/evidence/blur-run.log`).

Treat the figures as exploratory.

### 2.1 Cold start, one-shot process

| Path | First frame committed | Notes |
|---|---|---|
| C, wl_shm CPU fill | **3.6 ms** | presented 24.6 ms (vblank-quantised) |
| C, raw Vulkan clear | 15.6 ms | instance 4.6 + device 6 ms |
| C, EGL + GLES2 clear | 34.2 ms | `eglInitialize` ≈ 23 ms; outliers to 108 ms |
| Rust, wgpu (Vulkan) clear | 30.6 ms | device ≈ 14 ms |
| Rust, wgpu + vello panel | 71.9 ms | first run ever 968 ms (shader compile) |
| Rust spike (wgpu + panel shader + glyphon) | 52–74 ms | |
| Rust, sctk + tiny-skia + cosmic-text (CPU), font via fc-match | **25.7 ms** | fonts ready 16.5 ms; 3.7 MB stripped |
| same, `cosmic_text::FontSystem::new()` (full scan) | 140 ms | first run ever 1030 ms; fontdb has no cache |
| Go prototype (servitor research), CPU | 22–28 ms | pure Go, no cgo |
| fuzzel 1.15.0 | 22–38 ms | |
| GTK4 4.22 window | 79 ms | cairo renderer 61 ms |
| Qt Quick (Quickshell 0.3.1, layer-shell QML) | 152 ms | software backend 94 ms |

With CPU rendering, **language is not the startup bottleneck**. Every CPU stack lands at 22–28 ms, dominated by font discovery (an `fc-match` subprocess ≈ 10 ms; in-process libfontconfig is cheaper) [m]. tofi documents ~2 ms best case with a direct font path [land].

### 2.2 Resident daemon (Rust spike, `SPIKE=daemon`)

| Measure | Result |
|---|---|
| Daemon ready after exec | 21 ms |
| First open (GPU device created lazily) | 44.8 ms |
| Later opens, client request → committed (7 runs; each creates the layer surface + swapchain) | **6.5–8.9 ms**; a rerun with blur and a fixed drop order gave 6.0–6.9 ms (first open 36 ms) |
| RSS idle / after first open | 14 MB / **27 MB** (8.5 MB anonymous); your Quickshell: 62 MB |

### 2.3 Per-frame cost (spike, animated)

"GPU" here is host time spent waiting for completion after submit (`device.poll(wait)`), not timestamp-query execution time, and it excludes the compositor's blur and composition [2nd].

| Surface | CPU encode (median/p99) | GPU wait (median/p99) |
|---|---|---|
| 894×530 panel, scale 1.3 | 0.13 / 0.30 ms | 1.1 / 1.6 ms |
| 1920×1200 fullscreen, dimmed backdrop | 0.14 / 0.29 ms | 2.1 / 2.5 ms |

Compositor blur via `ext_background_effect_manager_v1` works on niri (capability `Blur` reported, `spike-blur.png`).

## 3. Landscape lessons [land]

- Instant-feeling launchers either stay lean and one-shot with CPU drawing (tofi, fuzzel, kickoff) or run as **resident daemons** where opening is IPC (walker, anyrun, vicinae, hyprlauncher, Quickshell shells).
- The best-looking results are GPU scene graphs: Qt Quick/QML with custom shaders (vicinae, DankMaterialShell, caelestia, end-4), GTK4 CSS (walker), and terminal post-processing shaders (Ghostty custom shaders, rio CRT filters). Those last two are the closest references for a CRT/grimdark look.
- Everyone uses libxkbcommon.
- Go appears as **backend daemons** (walker's elephant, now "on hold"; DankMaterialShell's dms), never as a maintained renderer. Pure-Go front ends are hobby-scale and dlopen libxkbcommon via purego.
- iced was dropped by centerpiece for loading every system font at startup (~550 ms versus ~190 ms for egui).
- Provider models open to an external CLI: pop-launcher (JSON over stdio), Ulauncher (JSON lines over a socket), rofi script mode.

## 4. Language evaluation

| | Rust | Go | C | Zig | C++ / Qt Quick |
|---|---|---|---|---|---|
| Wayland client + all modern protocols | sctk 0.21 + wayland-protocols 0.32.13 (ext-background-effect, color-management, cursor-shape, xdg-activation, fifo, commit-timing, drm-syncobj…) [src] | pure-Go wire libs (xogas, dankgo) **cannot hand a `wl_display*` to GPU drivers**; GPU needs libwayland via cgo/purego or a self-built dmabuf presentation path [m: wgpu needed wayland-rs `system`] | libwayland + wayland-scanner | C libwayland via `@cImport`; zig-wayland | LayerShellQt / Quickshell modules |
| GPU | wgpu 30 (Vulkan/GL/llvmpipe), ash, glow | cogentcore/webgpu (cgo → wgpu-native; 67★, no releases) [src]; Gio (cgo, **no layer-shell**, internal backend) [src]; gogpu (young, no layer-shell) | Vulkan/GLES directly | Vulkan/GL via C | Qt RHI scene graph, ShaderEffect |
| Text | cosmic-text 0.19 + swash (**TrueType hinting on by default** [src]), glyphon 0.12 atlas, parley | go-text (shaping, **no hinting**), or FreeType/HarfBuzz via cgo | FreeType + HarfBuzz + fontconfig (fcft) | same C libs | Qt text (FreeType/HarfBuzz) |
| Keyboard | libxkbcommon (sctk), xkbcommon crate | xkb-go needs fixes (servitor research) or libxkbcommon via cgo/purego | libxkbcommon | libxkbcommon | Qt |
| SVG icons | resvg/usvg 0.48 (native) | no complete Go SVG renderer; resvg C API via cgo | librsvg/resvg C API | C APIs | QtSvg |
| Fuzzy matching | nucleo 0.5 (helix; last release 2024-04), fzf-like | fzf algo package | own/fzy | own | own |
| Desktop entries / icons | freedesktop-desktop-entry 0.8, freedesktop-icons 0.4 | small libs or own | own | own | own/Qt |
| Startup (measured) | 25.7 ms CPU one-shot; 6.5–8.9 ms daemon | 22–28 ms CPU one-shot | 3.6 ms bare shm | n/a | 152 ms cold; daemon fast |
| Memory safety / velocity | safe, expressive; slower compiles | safe, simple; cgo friction | unsafe, slow to build features | partial safety, pre-1.0 churn | unsafe C++, heavy runtime |
| Owner fit | new language for this project (*assumption*) | **preferred**, matches servitor | — | — | — |

### 4.1 What a Go logis would take

Feasible with cgo:
- libwayland bindings with C callbacks (Gio's `os_wayland.c` shows the pattern);
- wgpu-native through cogentcore/webgpu, or Vulkan bindings;
- libxkbcommon and libfontconfig;
- go-text for shaping, plus our own glyph atlas, or FreeType/HarfBuzz through cgo for hinting;
- resvg through its C library (installed: `/usr/lib/libresvg.so.0.48`).

Every layer would be an FFI binding we own or a small, young dependency. Nothing in Go provides layer-shell with GPU rendering. *Inference:* roughly 2–3× the foundation work of the Rust path, with more cross-language debugging. A "Go everything except the renderer" split (a Go daemon plus a Rust renderer process) is possible; walker + elephant did exactly that [land]. It doubles the toolchains and puts a protocol between the logic and every frame, and the elephant half is now on hold. Not recommended.

**The strongest Go option is a different one: Go + gotk4 + GTK4 + gtk4-layer-shell, run as a daemon** [2nd]. GTK supplies text, input methods, accessibility and widgets; gotk4 is active (pushed 2026-08, 706★).

Its cost for this project:
- Custom shader effects need a `GtkGLArea` with our own GL renderer inside it. GTK 4.16 deprecated `GskGLShader` for `GtkGLArea` [src: `/usr/include/gtk-4.0/gsk/gskglshader.h`]. CSS alone cannot express glow, scanlines or grain animation.
- A 61–79 ms cold start, so it must run as a daemon.
- Native packaging and binding maintenance.
- walker, the best-known GTK4 launcher, moved from Go/gotk4 to Rust in 2025 [land].

Choose it only if accessibility, IME and widgets matter more than owning the look.

### 4.2 Others considered

- **C:** the fastest bare path (3.6 ms) and fuzzel/foot prove it, but slow feature velocity and no memory safety for a long-lived, input-handling daemon.
- **Zig:** Ghostty proves the approach, but the language is pre-1.0 with yearly breaking releases, the ecosystem is thin, and the toolchain isn't installed.
- **C++/Qt Quick:** the fastest route to "looks amazing", with QML as a ready-made theme language and shaders. Cost: C++, a heavy runtime, 152 ms cold start (daemon mandatory), 60+ MB resident, and a theming model owned by Qt rather than by logis.
- **GTK4 (any language):** CSS theming cannot express shader effects; 61–79 ms cold start. (See §4.1 for the Go/gotk4 variant.)
- **A clean-room QML frontend inside your running Quickshell, with Go doing provider work** [2nd]. Probably the shortest route to polished visuals. Opening a panel inside the already-running shell avoids the 152 ms process cold start (not measured here). But it couples logis to your shell's lifecycle and config, needs a proper dmenu response channel, and is not a standalone program.
- **Dart/Flutter, Electron/Tauri, Python:** runtime weight and startup are disproportionate for a launcher.
- **Odin/Nim/Hare:** ecosystems too thin for Wayland + GPU text.

## 5. Foundations in detail (Rust)

### 5.1 Process model

- `logis daemon`: owns the Wayland connection, GPU device, font and glyph caches, app index and providers. It shows and hides layer surfaces on request. It should create the GPU device at startup (the spike created it lazily; its first open was 44.8 ms against 6.5–8.9 ms warm).
- `logis` client (`--dmenu`, `--apps`, `--provider servitor`, …): connects to `$XDG_RUNTIME_DIR/logis.sock`, streams items or the stdin lines, waits, prints the result, exits with dmenu codes. If no daemon is reachable, it either starts one (systemd socket activation or a fork) or falls back to a one-shot render.
- Client ↔ daemon protocol: a **framed Unix stream** with explicit session, completion and cancellation. The spike's fire-and-forget datagrams only measured opening [2nd]. Define the behaviour for concurrent callers, client disconnects (cancel the session), and dismissal (exit non-zero, print nothing). Preserve each input line's original bytes separately from its display text. Dismissing a picker must never kill an action that is already running (e.g. a servitor invocation).
- Lifecycle duties: exit and restart on compositor disconnect; reload config and themes on change; watch `.desktop` directories with inotify; launch apps into the session environment (systemd user env / uwsm) using xdg-activation tokens.

### 5.2 Wayland

- sctk 0.21 with the `system` backend: layer shell, seat/keyboard (libxkbcommon), pointer, shm, output, data device.
- `wayland-protocols` 0.32.13 for fractional-scale + viewporter (both proven in the spike), ext-background-effect (proven), cursor-shape, xdg-activation, presentation-time, single-pixel-buffer, fifo/commit-timing.
- `wayland-protocols-wlr` for layer-shell (via sctk).
- Event loop: calloop (sctk's default) or a hand-rolled poll loop like the spike's.

### 5.3 Rendering

Recommended: **wgpu plus logis's own renderer.** Primitives:
- SDF rounded rectangles, borders, glows and shadows in WGSL;
- glyph quads from glyphon's atlas;
- image quads from an icon atlas (resvg rasterises SVGs once per size);
- an optional fullscreen post-processing pass (bloom, scanlines, chromatic aberration, CRT curvature).

Damage tracking and frame-callback pacing keep idle frames at zero. A launcher's UI vocabulary is small (input line, list, rows, sections, a preview pane later), so a retained scene with a tiny layout engine suffices, and the theme engine can map directly onto these primitives.

Rejected alternatives:
- **vello:** 72 ms cold start, 968 ms first shader compile, pre-1.0 churn; more vector power than a launcher needs [m].
- **iced/libcosmic:** fonts-at-startup cost [land] and an opinionated widget/style model that limits custom shader looks.
- **GPUI:** churn, Zed-centric.
- **CPU-only tiny-skia:** fine for static themes, but animated fullscreen effects are out of reach (*inference*).
- **Raw Vulkan (ash):** half wgpu's device-init time (15.6 vs 30.6 ms) but far more code. Irrelevant in a daemon. Keep it as the escape hatch if wgpu churn hurts.

GPU fallback: enable wgpu's GL backend as a second choice; Mesa's lavapipe/llvmpipe covers GPU-less systems.

### 5.4 Text and fonts

- cosmic-text 0.19 (rustybuzz shaping, swash rasterisation with hinting, bidi, fallback) through glyphon 0.12 (GPU atlas, tracks wgpu 30).
- **Never** `FontSystem::new()` (a full scan, 140 ms per launch).
- Resolve families and fallbacks through libfontconfig in-process (`fontconfig`/`yeslogic-fontconfig-sys` crates; dlopen is possible) and load only those files.
- Use glyphon's `ColorMode::Web` with a non-sRGB swapchain, or `Accurate` with an sRGB one. The spike hit this mismatch.

### 5.5 Input, matching, desktop

- libxkbcommon via sctk (compose, repeat, keysyms). Editing still needs our own work: grapheme-aware cursor, clipboard, and text-input-v3 for IME preedit [2nd].
- nucleo for fzf-like ranking (0.5, last release 2024-04, used by helix; small enough to vendor if it stalls).
- freedesktop-desktop-entry, plus freedesktop-icons or our own icon-theme lookup (the spec is small).
- Launching correctly is more than splitting `Exec=`: visibility rules (`NoDisplay`, `OnlyShowIn`, `TryExec`), field codes, `Terminal=true`, `DBusActivatable`, activation tokens, systemd scopes. Options: a careful own implementation with tests, or GIO's `GDesktopAppInfo` behind an adapter (correct and battle-tested, but it pulls in GLib) [2nd].
- resvg for SVG icons.
- zbus only if a D-Bus surface is wanted.

## 6. servitor integration

- **A process boundary, not linking.** servitor's packages are all under `internal/`, so Go could not import them anyway [src: servitor `internal/`]. Invocations can elevate and run long; keeping them in servitor is right.
- **Provider protocol.** JSON lines over stdin/stdout of a provider process (pop-launcher style [land]), in both directions and incremental. Items carry a stable id, columns, icon or glyph, and badges. Actions are selected-id plus a stage transition, so a rite → aspect → inscription flow can run in one window. Progress and outcome messages stream back. The `dmenu` mode remains for scripts.
- **First-class** means logis ships the servitor provider and a matching theme vocabulary: standing glyphs, aspect badges, vox-cast-style toasts. The provider uses `census --binharic` (JSON), `augury <rite>` (JSON by default) and `invoke`.
- **Correction** [2nd, verified]: `servitor invoke` currently reports only narration and an exit status, with no JSON outcome (`internal/cli/invoke.go` has no binharic rune). Until servitor adds one, the provider uses the exit status plus a follow-up `augury`, and never parses narration.
- servitor-side asks for true first-class integration: a probe-free fast census, inscription metadata (mandatory/decrees per aspect), and a structured invoke outcome.
- **Problem found:** `servitor census` (the CLI) calls `Census(false)`, which runs every rite's **auspex sequentially** [src: servitor `internal/rituals/census.go`, `internal/cli/census.go`]. A Librarium with slow probes would stall a blocking menu by the sum of their runtimes (each up to its 2 s patience). Required: logis renders provider items as they stream and updates rows asynchronously. servitor should gain a fast path (exposing the existing `forgoAuspex`) or a streaming census. On the 4 example rites, which have no auspex, `census --binharic` took 20–29 ms [m].

## 7. Risks and mitigations

| Risk | Kind | Mitigation |
|---|---|---|
| wgpu majors arrive about quarterly (30.x 2026-08); glyphon pins them | plausible maintenance cost | pin, upgrade deliberately; renderer surface kept small; ash as escape hatch |
| GPU unavailable or broken (old GPUs, VMs, driver bugs) | plausible | wgpu GL backend; lavapipe; a one-shot CPU fallback is optional |
| Daemon lifecycle: compositor restart, environment for launched apps, stale index, first open after socket activation (~50–75 ms) | plausible | exit-and-restart policy; systemd user env/uwsm; inotify; pre-create the GPU device at daemon start |
| servitor census stalls on auspex probes | **demonstrated in code** | show cached results with freshness, refresh asynchronously, never gate opening or typing; servitor fast path |
| A custom renderer means owning UI infrastructure: IME preedit, bidi cursor movement, selection, clipping/ellipsis, fallback fonts, accessibility | **high, inherent** [2nd] | scope it per release; glyphon/cosmic-text cover shaping and bidi layout; accessibility via AccessKit later if needed |
| Native lifetimes: wgpu surfaces from raw handles must drop before their wl_surface | **demonstrated** (the spike had the order wrong; fixed) | encode destruction order in types |
| Surface lifecycle: resizes, scale and output changes after the first configure | demonstrated gap in the spike | a real configure/scale state machine |
| Present mode: the spike hard-codes Mailbox; only FIFO is guaranteed; the spike blocks on GPU completion each frame | demonstrated in spike code | negotiate present mode; no blocking waits in the frame loop |
| Owner learning curve in Rust | owner factor | contained foundation crates; this research's spikes as references |
| Text: cosmic-text full scan if misused | demonstrated | fontconfig-driven loading only |
| Multi-GPU / NVIDIA proprietary | untested | wgpu adapter selection; test before v1 |

## 8. Independent review

Codex CLI `gpt-6-astra`, high effort, read-only, **neutral briefing** (requirements, measurements and artifacts, without this document or its verdict). Session `01a12a2a-d8ea-7783-894e-84d73a63c29b`; files in `~/.cache/logis-research/consult/`.

It independently recommended the same stack: Rust, sctk/wayland-rs (system backend), wgpu, cosmic-text/glyphon, and a resident process with a CLI client. It framed the justification as foundation availability, not Go being slow, and conditioned it on your willingness to maintain Rust.

Each of its corrections was verified and folded in, marked [2nd]:
- the GPU-time label;
- the comparability of the timing methods and the 147 ms tail;
- `invoke` having no JSON outcome;
- the spike's drop-order bug (fixed);
- framed dmenu sessions;
- launch correctness;
- the custom-renderer UI cost;
- two Go/QML alternatives (§4.1, §4.2).

It also noted that CPU rendering remains valid for beautiful static typography and ornament; GPU is what continuous animation needs.

## 9. Decisions for the owner

1. Rust (recommended) or Go with cgo (§4.1 lists what that implies).
2. Daemon + thin client (recommended), with or without systemd socket activation and a one-shot fallback.
3. Whether to request the servitor-side fast/streaming census (tracked in servitor, not logis).

## Limitations

- One machine, one GPU (Intel Arc, Mesa), one compositor (niri). NVIDIA and AMD are untested.
- Small samples (n = 5–10). The latency figures are indicative.
- The spike exercises rendering, text, scale, blur and daemon latency. It does not exercise keyboard input, pointer, app indexing or the provider protocol.
- The Go column relies on the earlier servitor research and source reading. No Go GPU spike was built.
- Rust 1.91.1 is the installed toolchain. All crates chosen built with it (the prototypes pin `rust-version = "1.91"`).
