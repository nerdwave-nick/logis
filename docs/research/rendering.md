# Research: rendering basis (cgo GPU vs pure-Go CPU)

Date: 2026-10-12. Machine: the owner's (Intel Core Ultra 7 155H, 22 threads; Intel Arc Meteor Lake iGPU, i915), Arch Linux, niri 26.04, Mesa 26.2.4, wayland 1.26.0, wayland-protocols 1.49, Go 1.27.2. eDP-1 1920×1200 @ 60.026 Hz, scale 1.3 (logical 1476×923).
Tracker: lit project `feat/logis-go`, ticket 9e8165d7 "Research: Go rendering basis (cgo GPU vs pure-Go CPU)", feeding decision ticket 9189f099 (spec §8 D1, and with it D5).
Branch `research/go-rendering` (base `main` 2ffdd02). Evidence code: `research/go/render-cpu/`, `research/go/render-gpu/`, `research/go/render-bench/` (raw results in `results/*.jsonl`). Screenshots: [`docs/evidence/`](../evidence/).

Tags: **[m]** measured here, **[src]** read in source, *inference* = judgement.

## Question

Which rendering basis should logis use?

- **(a) cgo GPU:** OpenGL ES or Vulkan through our own thin cgo bindings and our own shaders, presenting into a layer-shell surface; traditionally with libwayland-client via cgo, because EGL/Vulkan WSI wants a `wl_display*`.
- **(b) pure-Go CPU:** our own Wayland wire protocol, `wl_shm` buffers in a memfd, our own SDF/coverage rasteriser in Go.

Both must draw the forge-crimson-brass panel of the design canvas at 820×520 logical px on niri at scale 1.3 (fractional-scale + viewporter) with the continuous effects (glow pulse, travelling border highlight, scanlines, grain, vignette; flicker), phosphor bloom and curvature if feasible, as a **resident** daemon that shows and hides the panel on request, measured against the budgets (spec §3: warm open ≤ 15 ms, keystroke → frame within one frame, first open ≤ 150 ms, idle without wake-ups, RSS ≤ 64 MB).

## Verdict

**D1: (a), GPU through cgo to EGL + OpenGL ES 3 only. D5: our own pure-Go Wayland wire protocol; libwayland is not needed.** Concretely the third prototype built here, `render-gpu-dmabuf`: Mesa's surfaceless EGL renders into textures, which are exported as dmabufs and presented with `zwp_linux_dmabuf_v1` over the same pure-Go wire protocol the CPU prototype uses.

- It meets every budget with wide margins [m]: warm open **1.8–4.2 ms** median (budget 15), keystroke → commit **1.0 ms**, **0.1–0.2 ms** of CPU per animated frame, **2–4 %** of one core while the effects run, no CPU time and no wake-ups when idle or hidden, RSS 57–62 MB (budget 64), first open 88–114 ms (budget 150).
- It draws exactly what (b) draws: the GPU frame and a direct per-pixel Go port of the same shaders differ by at most 2 of 255 levels [m]. Bloom and curvature (the phosphor look) cost the CPU nothing.
- (b) works for the forge look, but only through heavy, effect-specific caching. Even then it sits at the edge of the budgets under load. Bloom and curvature are out of reach. Details follow.

The two explicit statements the owner asked for:

1. **Choosing (b) would revise feat/logis's "no CPU renderer" decision.** The owner chose a GPU-only look and declined CPU-only rendering (owner decision on ddba629b, now recorded in [ADR 0002](../adr/0002-go-and-a-small-cgo-set.md)); [`CONTEXT.md`](../../CONTEXT.md) says "There is no CPU renderer." The recommended (a) keeps that decision.
2. **(a) does not force libwayland.** EGL's Wayland platform and Vulkan's `VK_KHR_wayland_surface` do need a libwayland `wl_display*`. A GPU renderer does not need either: render offscreen, export the image as a dmabuf, and hand it to the compositor over our own wire protocol. `render-gpu-dmabuf` does exactly this. Its binary links only libEGL, libGLESv2 and libc (`ldd`), and our code makes no libwayland call. One caveat: Mesa's EGL driver itself links libwayland-client, so `libwayland-client.so` is still *mapped* into the process (seen in `/proc/<pid>/maps`), unused. **D5 is therefore free:** own wire protocol, whichever renderer is chosen.

## 1. Prototypes

All three are resident daemons. A Unix datagram socket takes `show <t0 ns>`, `hide`, `stats` and `quit`. Each show creates a layer surface (overlay, fractional-scale + viewporter, the first frame already at 1.3); each hide destroys it. Frames are paced by frame callbacks. Input (Up/Down in the nested compositor) is drawn at once rather than waiting for the animation's next callback. They share `scene` (the forge layout, palette and animation terms) and the stand-in text (`glyph`: x/image/font/opentype outlines into one alpha atlas; Noto Sans Mono and Noto Serif, which is what fontconfig gives on this machine for IBM Plex Mono and Cinzel).

| | (b) `render-cpu` | (a) `render-gpu` | (a2) `render-gpu-dmabuf` |
|---|---|---|---|
| Wayland | own wire runtime + XML generator (pure Go) | libwayland-client via cgo; protocol glue from wayland-scanner | own wire runtime (shared with b) |
| Buffers | 3 × `wl_shm` in one memfd pool | Mesa's EGL WSI (wl_egl_window) | 4 GL textures exported as dmabufs (`EGL_MESA_image_dma_buf_export`), `zwp_linux_dmabuf_v1` |
| Renderer | Go port of the panel's SDF and post-processing shaders, factored (below) | same shaders in GLSL ES 3.00, offscreen pass + post pass | same as (a) |
| GPU API | — | EGL (Wayland platform) + GLES 3, own cgo bindings | EGL (Mesa surfaceless platform) + GLES 3 |

**Fidelity [m].** `raster.Ref` is a direct per-pixel port of the two shaders (SDF panel and post-processing). The GPU prototype rendered offscreen differs from it by at most **2 levels** (mean 0.18, forge and phosphor; `render-gpu/gles/gles_test.go`). So both prototypes draw the same picture.

The CPU prototype's fast renderer is not the direct port. It is a factoring of it, needed for speed (§3.2):

- forge: within **1 level** of the port;
- with bloom and curvature: within 39 levels next to the moving highlight (bloom sees the border at rest), mean 0.43 levels;
- dirty-rectangle updates equal full rebuilds exactly (`render-cpu/raster/raster_test.go`).

**The look.** [`canvas-main.png`](../evidence/canvas-main.png) is the design canvas (`Main.dc.html` rendered in headless Chromium, with its real Plex Mono/Cinzel); [`cpu-forge.png`](../evidence/cpu-forge.png) and [`gpu-forge.png`](../evidence/gpu-forge.png) are the two prototypes on the live niri at 1.3.

Both prototype panels show the canvas's geometry, gradients, rivets, rule ornament, selection band, glow, scanlines and vignette. Differences from the canvas: Noto instead of Plex Mono/Cinzel, a round rule ornament, no badges, and a 7-row stand-in list.

The prototypes do not request compositor blur (`ext-background-effect`). That is one protocol object, not rendering work.

Further shots in [`docs/evidence/`](../evidence/): `cpu-phosphor.png` and `gpu-phosphor.png` (bloom and curvature, identical on both); `dmabuf-forge.png`; `cpu-fullscreen.png` and `gpu-fullscreen.png`.

## 2. Method

The research/bench `T0_NS` method ([`research/README.md`](../../research/README.md)): the caller takes CLOCK_MONOTONIC (`T0_NS` before exec, or the stamp in `show <t0>`), and the daemon stamps its milestones with the same clock. The driver is `research/go/render-bench/bench.py` on the research branch.

- **Warm open:** show request → first `wl_surface.commit` flushed, live niri, n = 20 after one warm-up show; each show starts 250 ms after the previous hide.
- **First open:** exec of the daemon with `-show` → first commit, n = 10, with its milestones.
- **Keystroke → commit:** in a **nested niri** started with a generated config (scale 1.3, never the owner's config, own runtime dir); Up/Down from `render-cpu/cmd/vkbd` (`zwp_virtual_keyboard_v1`; it refuses any socket outside the nested runtime dir), stamped just before the press is written; n = 20; with effects static and with continuous effects.
- **Per frame:** the daemon's own timing of its frame work (CPU), 5 s ≈ 300 frames, plus the interval between consecutive commits. GPU execution time from `GL_EXT_disjoint_timer_query` (non-blocking), with the GPU clock sampled from i915's `gt_act_freq_mhz`.
- **CPU and wake-ups:** utime + stime of the daemon, and the sum of voluntary + involuntary context switches over all its threads, over 5 s: visible with effects, visible with effects off, hidden. The compositor's CPU over the same window.
- **RSS:** VmRSS/VmHWM after the visible window.

Every round ran as one command under the shared lock (`flock … measure.lock`), load average printed before and after. Each JSON line in `results/` carries the load at its time.

**Caveats.**

- The machine was busy throughout: other agents' builds and test suites and a QEMU VM gave load averages of **5–27** (22 threads). The load is given with every number.
- The CPU prototype is far more load-sensitive than the GPU ones (it needs CPU time per frame).
- Meteor Lake mixes P, E and LP-E cores, so unpinned timings vary.
- Round r1 ran before a buffer-release bug was fixed (`6ba26da`). Its CPU "off" opens and dmabuf rows are invalid and left out. r1b and r1c are the valid repeats.
- The nested niri's winit backend blocks on its parent's vsync while it composes an animated client. With continuous effects it delivers keys late (≈ 13 ms) to every client. Those totals measure the nested compositor more than the renderer, so receipt → commit is given too.
- "Committed" is not presented.

## 3. Results

### 3.1 Latency

| Measure | (b) CPU | (a) GPU + libwayland | (a2) GPU + own wire, dmabuf | Budget |
|---|---|---|---|---|
| Warm open, forge, median / p95 (r1b, r1c) | 17.5 / 27.5 (load 18.0); 16.1 / 20.3 (load 7.4) | 2.6 / 3.3 (11.4); 2.7 / 3.4 (8.5) | 2.1 / 3.8 (12.4); 4.2 / 6.7 (9.1) | 15 |
| Warm open, effects off (n = 3) | 9.1 (16.6); 31.4 (10.1) | 9.0 (12.4); 6.2 (9.7) | 5.1 (9.9); 1.8 (6.2) | 15 |
| Warm open, phosphor (bloom + curvature) | **361.9** (16.0); **268.0** (8.4) | 16.6 (12.5); 3.0 (9.7) | 1.9 (9.8); 1.9 (7.3) | 15 |
| Warm open, fullscreen forge (r2, n = 15) | **74.9** / 245 (10.2) | 3.7 / 4.9 (8.6) | 1.8 / 3.3 (6.5) | 15 |
| First open: exec → first commit, n = 10 (r3; r6) | 125.6 / 139.7 (6.3); **191.7** / 237 (6.5) | **317.4** / 375 (6.7); 57.3 / 75.6 (6.5) | 113.8 / 255 (6.4); 87.5 / 112 (7.4) | 150 |
| Keystroke → commit, effects static (nested, r4) | 2.90 / 4.37 (5.8) | 1.02 / 1.56 (5.4) | 1.00 / 1.22 (5.4) | 16.7 |
| Keystroke → commit, continuous effects: total; receipt → commit | 45.6; **15.1** / 20.4 (6.3) | 29.7; **16.7** / 17.0 (5.8) | 13.1; **0.11** / 0.23 (5.4) | 16.7 |

Values are medians / p95 in ms, with the load average in brackets.

First-open milestones (r6, ms after exec):

| Variant | connected | renderer ready | fonts | first commit |
|---|---|---|---|---|
| CPU | 5.3 | 6.2 | 22.8 | 191.7 |
| GPU + libwayland | 2.8 | 42.7 (EGL init + shader compile) | 52.3 | 57.2 |
| dmabuf | 2.2 | 66.6 | 84.1 | 87.5 |

The CPU path spends ~170 ms on its first frame: pixel classification, the cached empty panel, the static layer and the grain layer. The GPU path's 317 ms in r3 against 57 ms in r6 is not explained by load (6.7 vs 6.5); both are reported.

Keystrokes with continuous effects:

- (a2) answers in 0.11 ms.
- (b) needs a static update plus a full frame (15 ms median, p95 20 ms: over one frame at p95).
- (a) waits ~16.7 ms inside `eglSwapBuffers`. Mesa's WSI blocks until the compositor releases a buffer when a second frame is drawn within one vblank. EGL offers no control over that. (a2) solved the same starvation by owning four buffers.

### 3.2 Per frame (continuous effects, 5 s visible, live niri)

CPU work per frame and the commit interval (median / p95, ms):

| Surface | (b) CPU, 4 goroutines | (a) GPU encode | (a2) dmabuf encode |
|---|---|---|---|
| Panel 1149×759 px, forge (r1b; r1c) | 4.78 / 29.2, gap 19.2 (17.0); 2.56 / 9.35, gap 16.7 (7.4) | 0.10 / 0.18 (11.3); 0.09 / 0.15 (8.3) | 0.19 / 0.34 (11.5); 0.19 / 0.57 (8.9) |
| Panel, phosphor (bloom + curvature) | 22.5 / 36.8, gap **30.0** (9.5) | 0.08 / 0.17, gap 16.7 (9.1) | 0.17 / 0.62, gap 16.7 (7.0) |
| Fullscreen 1920×1200 (r2) | 1.41 / 3.26, gap 16.6 (9.7) | 0.07 / 0.12 (8.1) | 0.19 / 0.28 (6.4) |

The CPU path's fullscreen frame is no dearer than the panel: only the panel and its glow change, and the dimmed backdrop is written once per buffer. Effects that covered the whole screen would scale with its 2.3 M pixels.

GPU execution time per frame (timer queries, r5, load 5.7–7.0; r1c, load 9.0):

| | (a) GPU + libwayland | (a2) dmabuf (LINEAR export) |
|---|---|---|
| Panel forge | 0.24 (r5); 4.17 (r1c) | 7.60 |
| Panel phosphor | 7.95 / 15.9 | 15.9 (gap 23 ms) |
| Fullscreen forge | 4.99 (gap 17.9 ms) | 15.7 (gap 27.7 ms) |
| Fullscreen phosphor | 16.7 (gap 29.9 ms) | 22.9 (gap 35 ms) |

The i915 GPU ran at **100 MHz**, its minimum (max 2250), in almost every sample. Two causes:

- **No wait-boost.** A client paced by frame callbacks never waits on the GPU, so i915's wait-boost never fires. Its load-based governor kept the floor clock even when frames ran late.
- **Linear export.** The dmabuf export came out LINEAR (`AB24`, modifier 0, one plane), and rendering to linear memory costs ~1.8× the tiled target Mesa allocates for its own swapchain.

So the heavier cases (fullscreen, phosphor, the linear dmabuf path) missed 60 Hz at the floor clock. The per-pixel shaders here are unoptimised: one quad per shape covering its glow, and the panel's quad covers the whole surface. Fixes (§5) are known. The CPU path runs its effects at 2–5 ms of CPU per frame on 4 goroutines, but costs the compositor an upload per frame.

### 3.3 Idle, CPU and wake-ups

| State (5 s) | (b) CPU | (a) GPU + libwayland | (a2) dmabuf |
|---|---|---|---|
| Visible, continuous effects, panel: daemon CPU, wake-ups/s, compositor CPU | 40–147 % of a core, 2 759–3 917 /s, niri 9.6–21.6 % (load 7–17) | 2.4–3.0 %, 2 539–3 027 /s, niri 5.6–7.0 % | 2.8–3.6 %, 3 494–3 807 /s, niri 6.2–8.0 % |
| Visible, phosphor | 212–215 %, niri 28 % | 2.2–11 % | 3.0–9.2 % |
| Visible, fullscreen forge (r2) | 32 %, 4 379 /s, niri 8.2 % | 2.0 %, 2 374 /s, niri 5.6 % | 2.6 %, 3 726 /s, niri 6.4 % |
| Visible, effects off | 0.0 %, 9–11 /s | 0.0 %, 5–8 /s | 0.0 %, 0–9 /s |
| Hidden | **0.0 %, 0 /s** | **0.0 %, 0 /s** | **0.0 %, 0 /s** |

Idle meets the budget everywhere, and continuous effects are explicitly excepted by it. The visible-with-effects wake-ups are a **Go runtime property**, not the renderer's: one thread (sysmon) wakes 1 300–3 600 times a second. Reproduced with a 30-line program [m]: a 60 Hz timer loop costs ~180–300 wake-ups/s; add 40 short cgo calls or a 4-goroutine fan-out per tick and it costs 1 300–3 600/s. With `GOMAXPROCS=1` it costs 370–810/s. sysmon goes back to deep sleep once nothing runs, hence 0 when idle. The CPU cost is small (2–4 % in total for the GPU daemons).

### 3.4 Memory, binary, build

| | (b) CPU | (a) GPU + libwayland | (a2) dmabuf |
|---|---|---|---|
| RSS after visible (MB) | 50–56 panel; **66 fullscreen**; 44–46 effects off | 55–61 | 57–62 (of which ~35 MB file-backed: Mesa's driver) |
| Binary (default / `-s -w`) | 4.04 / 2.54 MB | 3.46 / 2.25 MB | 3.54 / 2.31 MB |
| Linking (`file`, `ldd`) | statically linked (no cgo used) | dynamic: libwayland-client, libwayland-egl, libffi, libEGL, libGLESv2, libGLdispatch, libc, libm | dynamic: libEGL, libGLESv2, libGLdispatch, libc |
| Clean build (fresh GOCACHE incl. std, load ~8) | 3.9 s | 5.8 s | 5.5 s |
| Build needs | Go | Go + cgo (gcc, pkg-config, wayland/EGL/GLES headers) + `wayland-scanner` at `go generate` | Go + cgo (gcc, pkg-config, EGL/GLES headers) |
| Shader step | — | none: GLSL compiled by the driver at start (Mesa's disk cache) | same |
| Go modules beyond std | x/sys, x/image (+ x/text linked through x/image's sfnt) | same + the render-cpu module | same |

The CPU path's memory goes into float layers: P, the pre-post content at 16 bytes per pixel, plus cached layers for the empty panel, grain and bloom. Lower-precision layers would bring the fullscreen case back under 64 MB. *Inference:* the GPU paths' RSS is mostly Mesa's mapped driver and is roughly constant.

### 3.5 Lines hand-written per layer

Non-blank, non-comment lines [m]:

| Layer | Lines | Note |
|---|---|---|
| Wire runtime (socket, encode/decode, SCM_RIGHTS, dispatch) | 316 | shared by b and a2 |
| XML → Go generator | 252 | generates 2 108 lines for 6 protocols |
| CPU rasteriser | 982 | SDF/paint/hash primitives 149; direct shader port (oracle; curvature sampling) 168; the factoring: cached layers, dirty rectangles, grain epochs, bloom sums 665 |
| CPU daemon (memfd pool, window, loop) | 460 | |
| libwayland binding: C glue + Go wrapper | 269 | + 1 317 generated by wayland-scanner |
| EGL + GLES binding and GL renderer | 391 | + 61 for wl_egl_window (a only) |
| GLSL ES shaders | 271 | the SDF panel, the post-processing pass, a glyph pass |
| GPU daemon, libwayland | 291 | |
| GPU daemon, own wire + dmabuf | 404 | |
| Shared scene / stand-in text / control | 242 / 164 / 95 | |
| Tests / vkbd injector | 377 / 106 | |

Totals for a full renderer + Wayland layer:

- **(b)** wire 316 + generator 252 + rasteriser 982 + daemon 460 ≈ **2 000**.
- **(a2)** wire 316 + generator 252 + GL binding/renderer 391 + shaders 271 + daemon 404 ≈ **1 630**, with no effect-specific factoring.
- **(a)** binding 269 + scanner glue + GL 452 + shaders 271 + daemon 291 ≈ **1 280**, plus libwayland's C API to understand.

## 4. Analysis

**(b) pure-Go CPU.** It is feasible for the forge look, but it only works because the effects decompose into what never moves (panel, text, scanlines, vignette, a cached empty panel) and a few cheap per-frame terms (flicker × pixel, 24 Hz grain, the border ring and glow). The direct port (every pixel, every frame) costs 144 ms per frame for forge and 1.19 s with bloom and curvature, single-threaded in Go (load 13).

That decomposition is effect-specific engineering:

- Every new effect or animation needs its own factoring.
- Bloom and curvature couple pixels, so they defeat it. A warm open with them costs 268–362 ms and frames run at ~33 fps.
- Even the forge look sits near the budgets under load: warm open 16–17.5 ms at load 7–18 (13.5 ms at 13.7 in r1); keystroke p95 20 ms while animating; first open 126–192 ms; fullscreen warm open 75 ms; fullscreen RSS 66 MB.
- It costs 40–150 % of a core while animating, and the compositor 10–22 % for shm uploads.
- *Inference:* most of these can be improved (scatter-based bloom, 8-bit layers, lazy glyphs, parallel classification). Each is more renderer to own.

Its strengths: a static binary, no cgo, one toolchain, every layer in Go.

**(a) with libwayland.** The simplest to write (269 lines of C glue plus scanner output) and fast: warm open 2.6–3.7 ms, keystroke 1.0 ms. But:

- it adds libwayland-client, libwayland-egl and libffi;
- it adds a scanner step and C callbacks into Go;
- Mesa's WSI owns buffers and pacing: `eglSwapBuffers` blocked ~16.7 ms for an input frame; Mesa makes dmabuf-feedback round trips per window surface; and it sends a `wl_display.sync` after every swap (`WAYLAND_DEBUG`).
- Every Wayland feature we want (layer-shell, xdg-activation, cursor-shape, data-device, ext-background-effect, presentation-time) would be cgo glue.

**(a2) GPU with our own wire protocol.**

- One Wayland implementation in Go, shared with any future CPU path, and cgo confined to EGL + GLES (≈ 450 lines).
- Effects cost the CPU nothing.
- It owns its buffers: four, so input never waits; and the frame pacing.
- Its warm open is 1.8–4.2 ms.

The price:

- cgo, so a C toolchain at build and a dynamically linked binary;
- the `runtime.LockOSThread` discipline. A GL context is current on one OS thread, and our test hit exactly this: a `t.Run` subtest drew on another thread, and nothing was drawn;
- Mesa-specific extensions (`EGL_MESA_platform_surfaceless`, `EGL_MESA_image_dma_buf_export`);
- implicit sync;
- buffer allocation and modifiers left to us.

## 5. Risks

| Risk | Applies to | Kind | Mitigation |
|---|---|---|---|
| The dmabuf export path is Mesa-specific; NVIDIA's proprietary EGL support for `EGL_MESA_image_dma_buf_export`/surfaceless is **untested** | a2 | plausible | test early on NVIDIA/AMD; fallback to (a)'s EGL Wayland platform on the same renderer, or Vulkan external memory (`VK_KHR_external_memory_fd` + `VK_EXT_image_drm_format_modifier`, no WSI; *inference*, not built) |
| Implicit sync: relied on here (glFlush, then commit). NVIDIA needs explicit sync (`linux-drm-syncobj-v1`) | a2 | plausible | `glFinish` fallback flag exists; explicit sync via EGL native fence fds (`EGL_ANDROID_native_fence_sync`) is pure protocol + cgo |
| The export came out LINEAR: ~1.8× GPU time vs tiled | a2 | **demonstrated** | allocate tiled buffers honouring the compositor's dmabuf feedback: via libgbm (cgo, one more C library), or EGL import of modifiers; or render tiled offscreen and blit once to the linear export |
| GPU stays at its floor clock (no wait-boost for a non-blocking client); heavy frames miss 60 Hz | a, a2 | **demonstrated** (fullscreen, phosphor, linear) | make the shaders cheaper (quads per region instead of whole-surface quads; skip highlight/glow maths in the interior); tiled targets; *inference:* a deliberate fence wait would boost at some CPU-wait cost |
| Multi-GPU: surfaceless EGL picks a render node by itself | a2 | plausible | choose the device from dmabuf feedback `main_device` (`EGL_EXT_device_enumeration`/`EGL_PLATFORM_DEVICE_EXT`) |
| No GPU / VM: lavapipe/llvmpipe with surfaceless EGL and dmabuf export | a2 | untested | test llvmpipe; else keep a minimal shm fallback (the wire layer is shared) |
| Go runtime wake-ups while animating: 2 400–4 400/s (sysmon) | all | **demonstrated** | idle is 0, as budgeted; `GOMAXPROCS=1` → 370–810/s; *inference:* blocking in the netpoller rather than raw `poll(2)`, and fewer cgo calls per frame, help |
| Thread affinity of GL contexts | a, a2 | **demonstrated** (a test drew nothing until fixed) | one locked render goroutine owning the context |
| CPU renderer: effect-specific factoring, bloom/curvature unaffordable, load sensitivity, fullscreen RSS 66 MB | b | **demonstrated** | — (reason to prefer a2) |
| Mesa WSI controls pacing and buffers (blocking swap, round trips per surface) | a | **demonstrated** | — (reason to prefer a2) |
| Nested niri delays keys to animated clients (~13 ms) | measurement | demonstrated | measure keystrokes with reduce-motion or on a real output |

## 6. Recommendation

**D1 (rendering): (a), cgo to EGL + OpenGL ES 3 through our own thin bindings and GLSL ES shaders compiled by the driver at start; no CPU renderer.** This keeps the "no CPU renderer" decision (now in ADR 0002), so nothing in feat/logis needs revising. Choosing (b) would have revised it. (b) remains *possible* for the forge look, at the price of a renderer that has to be re-engineered per effect, cannot do the phosphor theme's bloom and curvature within the budgets, and spends 40–150 % of a core while animating. Prefer GLES over Vulkan: the GLES prototype needs no shader build step and about 450 lines of binding, and EGL's start-up cost is paid once in a resident daemon.

**D5 (Wayland transport): our own pure-Go wire protocol with an XML generator; no libwayland.** This is demonstrated, not assumed: `render-gpu-dmabuf` presents the GPU renderer's frames as dmabufs over the pure-Go wire. It beats the libwayland variant on input latency (receipt → commit 0.11 vs 16.7 ms while animating) and links neither libwayland-client nor libwayland-egl. Mesa still maps libwayland-client into the process, unused. The wire runtime and generator are 568 lines and serve every protocol logis needs.

Before committing to it, three spikes are worth doing in implementation:

1. tiled buffer allocation that honours dmabuf feedback;
2. cheaper shaders, so heavy frames hold 60 Hz at the floor clock;
3. a run on NVIDIA and AMD, plus llvmpipe.

## Limitations

- One machine, one GPU (Intel Arc, i915, Mesa 26.2.4), one compositor (niri 26.04). Heavy and varying load from other work (5–27) throughout. CPU-path numbers in particular are load-sensitive. Samples: n = 20 opens, 10 first opens, 20 keystrokes, ~300 frames per series.
- The text is a stand-in: no shaping, no fallback, no hinting. The text ticket (W2) owns that. Font loading here is a direct path, not fontconfig.
- The prototypes draw a fixed 7-row scene: no list model, no open/close transitions, no blur, no icons.
- Keystroke timing used raw evdev codes (Up/Down) without an XKB keymap. Keyboard handling is ticket D3's.
- Vulkan was not built; its WSI-free path is inference from the extension specifications.

## Reproduce

```sh
# build (Go 1.27; cgo for render-gpu needs gcc, pkg-config, wayland/EGL/GLES headers)
cd research/go/render-cpu && go test ./... && go build -o /tmp/w1-bin/render-cpu ./cmd/render-cpu && go build -o /tmp/w1-bin/vkbd ./cmd/vkbd
cd ../render-gpu && go test ./... && go build -o /tmp/w1-bin/render-gpu ./cmd/render-gpu && go build -o /tmp/w1-bin/render-gpu-dmabuf ./cmd/render-gpu-dmabuf
# regenerate bindings: (cd research/go/render-cpu/proto && go generate); (cd research/go/render-gpu/cwl && go generate)
# one measurement round (live niri; keys only ever go to the nested niri it starts)
flock -w 3600 <lock> sh -c 'cat /proc/loadavg; python3 research/go/render-bench/bench.py live OUT.jsonl --runs 20; cat /proc/loadavg'
# rounds: live [--fullscreen], first, keys, gputime; raw results of this study: research/go/render-bench/results/
```
