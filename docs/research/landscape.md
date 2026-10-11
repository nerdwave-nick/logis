# Wayland launcher and terminal landscape (research for logis)

Survey date: 2026-10-11. Purpose: inform the choice of language and stack for a clean-room Wayland
launcher/picker. This is not a recommendation. Nothing here is forked or reused.

## Method and conventions

- Every repository was shallow-cloned into `~/.cache/logis-research/src/` and its build files read
  directly (Cargo.toml, meson.build, CMakeLists.txt, go.mod, build.zig.zon, pyproject.toml). Release
  tags and dates come from the GitHub/Codeberg APIs. The commit hashes are listed in the
  [source index](#source-index) at the end.
- "**Verified**" means it was read in a primary source (file path given). "**Inference**" means my
  own reading of the evidence, and it is labelled as such. Statements with no label are verified
  from the cited file.
- "Protocols" lists Wayland protocols that the project itself binds. When a toolkit such as GTK4,
  Qt, iced or winit binds them, the toolkit gets the credit, and that is labelled as inference
  unless I read the toolkit's source.

---

## 1. Summary tables

### 1a. Launchers / pickers

| Project | Lang | Toolkit | Wayland lib | Rendering | Text | Keyboard | Process model | Theming | Status / licence |
|---|---|---|---|---|---|---|---|---|---|
| fuzzel | C | none | libwayland (direct) | CPU: pixman (+ cairo optional) | fcft (FreeType+fontconfig, HarfBuzz optional) | libxkbcommon | one-shot | INI keys (colors, radius, border) | active, 1.15.0 (2026-09-02), MIT |
| tofi | C | none | libwayland | CPU: cairo on shm | HarfBuzz+FreeType direct (fast path) or Pango | libxkbcommon | one-shot, ~2 ms best case | INI keys, box model | dormant: last commit 2024-12-30, v0.9.1 (2023-04), MIT |
| wofi | C | GTK3 | GTK3 + raw wlr-layer-shell binding | GTK3 (cairo) | Pango | GTK | one-shot | GTK3 CSS | active (hg, last commit 2026-09-27), v1.5.3, GPL-3.0 |
| rofi 2.0 | C | none (own widgets) | libwayland (merged lbonn port) | CPU: cairo | Pango | libxkbcommon | one-shot | rasi (own CSS-like language, gradients) | active, 2.0.0 (2025-09-01), MIT/X11 |
| bemenu | C | none (lib + renderer .so plugins) | libwayland | CPU: cairo | Pango | libxkbcommon | one-shot | CLI flags / env | slow: 0.6.23 (2024-07), last commit 2025-08, GPL-3 client / LGPL-3 lib |
| anyrun | Rust | GTK4 + relm4 | gtk4-layer-shell | GTK4 GSK (GPU) | Pango (GTK) | GTK | one-shot or optional D-Bus daemon; plugins in a separate `anyrun-provider` process | GTK4 CSS | active, v26.6.1 (2026-06-09), GPL-3.0 |
| walker v2 | Rust (was Go until v0.13) | GTK4 | gtk4-layer-shell + wayland-rs for extra protocols | GTK4 GSK | Pango | GTK | resident GApplication service + Unix socket; data from the `elephant` daemon | GTK4 CSS (+SCSS via `grass`) + GtkBuilder XML per item type | active, v2.17.2 (2026-10-08), GPL-3.0 |
| elephant | Go | n/a (backend) | neurlang/wayland (pure Go) | n/a | n/a | n/a | resident daemon, protobuf over a Unix socket, Go `plugin` .so providers | n/a | **"Development is on hold"** (README, 2026-09-29), v2.22.1, GPL-3.0 |
| sherlock | Rust | GTK4 (main); GPUI rewrite branch | gtk4-layer-shell | GTK4 GSK; GPUI branch is GPU | Pango | GTK | one-shot or `daemonize` (socket) | GTK CSS | main branch last commit 2026-01; `gpui` branch active to 2026-04, GPL-3.0 |
| vicinae | C++23 + TS | Qt6 QtQuick/QML (since v0.20, 2026-02) | layer-shell-qt + own protocol code | Qt Quick scene graph (GPU) | Qt | Qt | resident server (`vicinae server`, systemd unit) + CLI client | TOML palette themes + QML | very active, v0.29.1 (2026-09-30), GPL-3.0 |
| hyprlauncher | C++23 | hyprtoolkit (own, Hypr) | hyprwayland-scanner (libwayland) | GPU: EGL/GLES3 | Pango/cairo, rasterised to textures | libxkbcommon | **daemon by default** | hyprlang config + toolkit theme | active, v0.1.6 (2026-04), BSD-3 |
| kickoff | Rust | none | smithay-client-toolkit 0.20 | CPU: shm, own drawing | fontdue + fontconfig | via sctk (libxkbcommon) | one-shot | TOML colours/font | maintained, v0.7.6 (2026-01), GPL-3.0 |
| yofi | Rust | none | sctk 0.18 | CPU: raqote | fontdue + fontconfig | via sctk | one-shot | TOML | low activity (last commit 2026-04; no real release since 0.2.2), MIT |
| onagre | Rust | iced 0.12 (wgpu) | via iced | GPU (wgpu) | via iced (cosmic-text) | iced | one-shot frontend that spawns pop-launcher | iced style config | stale: last commit 2025-04, 1.1.1 (2024-12), MIT |
| cosmic-launcher | Rust | libcosmic (iced fork) | via libcosmic (sctk layer-shell) | wgpu or tiny-skia | cosmic-text | libcosmic | single-instance + pop-launcher service | COSMIC theme | active (2026-10), GPL-3.0 |
| centerpiece | Rust | egui (iced until 2026-02) + `wayapp` | sctk 0.20 | GPU: wgpu | egui (ab_glyph-based; inference) | sctk | one-shot | YAML settings | active (2026-09), last release v1.1.1 (2024-08), MIT |
| gauntlet | Rust + TS | iced 0.13 fork (wgpu/tiny-skia) | sctk 0.19 | GPU/CPU via iced | cosmic-text (via iced) | iced | server + frontend; Deno plugin runtime | custom themes | **"no longer being developed"** (README), v21 (2025-08), MPL-2.0 |
| Ulauncher | Python | GTK3 (PyGObject) | GtkLayerShell (gtk-layer-shell) | GTK3 cairo | Pango | GTK | resident D-Bus service (systemd unit) | GTK3 CSS themes | active, v6.0.0-beta35 (2026-09), GPL-3.0+ |
| Albert | C++ | Qt6 (Widgets frontend plugin) | Qt | Qt | Qt | Qt | resident | Qt frontend themes | active, v35.1.0 (2026-07), **custom non-OSI "Albert license"** |
| Quickshell (framework) | C++ / QML | Qt6 QtQuick | libwayland via own modules (layer-shell, ext-background-effect, ...) | Qt Quick (GPU) | Qt | Qt | resident shell, IPC (`qs ipc`) | QML + GLSL shaders | very active, v0.3.2 (2026-10-08), LGPL-3.0 |
| DankMaterialShell | QML + Go | Quickshell | Quickshell + pure-Go wayland client (`dankgo`) | Qt Quick + custom `.frag` shaders | Qt | Qt | resident shell + Go `dms` daemon/CLI; `dms dmenu` | QML, matugen palettes, shaders | very active, v1.6.3 (2026-10-07), MIT (relicensed from GPL-3) |
| caelestia shell | QML + C++ plugin | Quickshell | Quickshell | Qt Quick + ShaderTools | Qt | Qt | resident | QML, M3 schemes | very active, v2.5.0 (2026-09), GPL-3.0 |
| end-4 dots (ii) | QML | Quickshell | Quickshell | Qt Quick | Qt | Qt | resident | QML | very active (2026-10), GPL-3.0 |
| Gofer (small) | Go | none | neurlang/wayland | CPU (golang.org/x/image) | x/image | libxkbcommon via purego dlopen | one-shot | TOML | new (2026-03), 8 stars |
| wlrun (small) | Go | none | rajveermalviya/go-wayland | CPU | x/image | libxkbcommon via purego | one-shot | TOML | new (2026-06), 0 stars |

### 1b. Terminals (rendering references)

| Project | Lang | Windowing | Rendering | Text | Keyboard | Process model | Theming / visuals | Status / licence |
|---|---|---|---|---|---|---|---|---|
| foot | C | libwayland direct | CPU: pixman, multithreaded render workers, damage tracking | fcft | libxkbcommon | one-shot or `foot --server` + `footclient` (socket-activatable) | INI; ext-background-effect | active, 1.28.0 (2026-09-02), MIT |
| kitty | C + Python (+ Go kittens) | own GLFW fork | GPU: OpenGL 3.1 (Linux) | FreeType + HarfBuzz + fontconfig | libxkbcommon | one-shot or `--single-instance` | conf file, kwin-blur + ext-background-effect; `panel` kitten uses layer-shell | active, 0.49.2 (2026-10-01), GPL-3.0 |
| alacritty | Rust | winit 0.30 (sctk) | GPU: OpenGL via glutin (EGL) | crossfont (FreeType + fontconfig on Linux) | xkbcommon-dl via winit | one-shot or `--daemon` + `alacritty msg create-window` | TOML | active, 0.17.0 (2026-04-06), Apache-2.0 |
| ghostty | Zig | GTK4/libadwaita (Linux) | GPU: OpenGL (Linux), Metal (macOS) | own stack on FreeType + HarfBuzz + fontconfig | GTK | GTK single-instance (D-Bus), default `detect` | config + Shadertoy-style GLSL `custom-shader`; ext-background-effect | active, v1.3.1 (2026-03-13), MIT |
| wezterm | Rust | own `window` crate (sctk on Wayland) | GPU: OpenGL (glium) default, WebGpu (wgpu) or Software | FreeType + HarfBuzz + fontconfig (vendored) | xkbcommon crate | one-shot, plus a mux server | Lua config | master active; last tagged release 20240203, MIT |
| rio | Rust | rio-window (winit fork) | GPU: native Vulkan (ash) is the default on Linux; wgpu optional (for librashader CRT filters); CPU context | font-kit + swash/skrifa/zeno, tiny-skia | xkbcommon-dl | one-shot | TOML; RetroArch shaders via librashader | active, v0.5.28 (2026-09-17), MIT |

---

## 2. Launchers, one section per project

### fuzzel
Repo: https://codeberg.org/dnkl/fuzzel @ `815d438` (2026-09-25). Release 1.15.0 (2026-09-02). MIT (`LICENSE`).

- **Stack** (`meson.build`): C. Deps are `pixman-1 >=0.46`, `wayland-client`, `wayland-cursor`,
  `xkbcommon`, `fontconfig` and `fcft >=3.3.1` (the same author's font library), with `cairo`, `libpng`
  and `librsvg` optional. No toolkit.
- **Protocols** (`meson.build` lines 153-164): wlr-layer-shell, xdg-shell, xdg-output, xdg-activation,
  cursor-shape, tablet-v2, viewporter, fractional-scale, primary-selection, color-management,
  single-pixel-buffer, text-input-v3 (IME support added in 1.15.0, `CHANGELOG.md`). It does **not**
  bind ext-background-effect.
- **Rendering**: CPU into wl_shm buffers using pixman. Render worker threads come from
  `render.c` (`thrd_create(... render_worker_thread)`, count set by the `render-workers` option).
  Matching also uses worker threads (`match.c`, `match-workers`).
- **Icons**: its own freedesktop icon-theme walk in `icon.c`. It reads `index.theme`, follows
  `Inherits` chains, falls back to `hicolor`, and also checks `/usr/share/pixmaps`. SVG backend
  options (`meson_options.txt`) are `nanosvg` (bundled, the default), `librsvg` (needs cairo) or
  `resvg` (C API). The local Arch build reports `+cairo +png +svg(resvg)`. PNG goes through libpng.
- **Launch** (`application.c`): an optional `launch-prefix` (for uwsm, app2unit and similar), and it
  sets `XDG_ACTIVATION_TOKEN` and `DESKTOP_STARTUP_ID` from an xdg-activation token. It parses
  `TryExec`, `Terminal`, `OnlyShowIn` and `NotShowIn`.
- **Startup trick** (1.15.0 changelog): "fuzzel no longer waits for all the `.desktop` files to be
  loaded before displaying the window". Apps are populated on a thread (`main.c`:
  `thrd_create(&app_thread_id, &populate_apps, ...)`). It keeps a frequency cache.
- **dmenu**: reads stdin and supports rofi's `\0icon\x1f<name>` row protocol, `--index`,
  null-separated input, password mode, and a prompt-only mode (README).
- **Theming**: INI `[colors]` and `[border]` sections with `radius` and `selection-radius`
  (`doc/fuzzel.ini.5.scd`). Visual range: flat colours, transparency, rounded corners. No gradients,
  shaders or animations.
- **Plugin model**: none. It is extended only through dmenu mode.

### tofi
Repo: https://github.com/philj56/tofi @ `1eb6137` (2024-12-30). Last release v0.9.1 (2023-04-10). MIT.

- **Stack** (`meson.build`): C with `freetype2`, `harfbuzz`, `cairo`, `pangocairo`, `wayland-client`,
  `xkbcommon` and `glib`/`gio-unix`. Protocols: xdg-shell, viewporter, wlr-layer-shell,
  fractional-scale.
- **Text**: two backends in `src/entry_backend/`. Pango+fontconfig is the default. When `--font` is a
  file path, it uses HarfBuzz and cairo directly.
- **Startup is the project's main topic and is documented in detail** (README "Performance"):
  - "when configured correctly, it can startup and get on screen within a single frame, or about
    2ms in the ideal case."
  - The biggest cost is font lookup. "with ~10000 fonts ... loading a single font with Pango &
    Fontconfig takes ~120ms." Passing a font file path skips fontconfig and makes font loading
    take "<1ms".
  - Window size matters because the first frame is a CPU paint. A 2880×1800 fullscreen window takes
    about 20 ms. A 60 px ribbon takes about 1 ms.
  - `--hint-font false` saves about 3-5 ms. `--ascii-input` skips Unicode handling.
    `--late-keyboard-init` defers xkb setup until after the first frame (1-2 ms on a laptop, up to
    60 ms on a Pi Zero 2 W), but keypresses are lost until then.
  - Transparent HugePages for shm (`shmem_enabled=advise`) cut fullscreen first-frame time
    (README table: 13.4 ms down to 7.9 ms).
  - Timeline annotations in `startup_performance.svg` (v0.8.0, dmenu theme): most of the remaining
    time is "Waiting for Sway" across three registry/configure roundtrips. Other annotated steps are
    "Reading the cache file (a single fread())" and "Initial Cairo paint - mostly waiting for memory
    from the kernel". The command list is precomputed into a cache (`src/compgen.c`,
    `~/.cache/tofi-compgen`).
  - Benchmarks (README, v0.1.0, measured from launch to Sway reporting the window on screen): dmenu
    theme 2.3-6.0 ms on a Ryzen, fullscreen 7.9-28 ms.
- **Icons**: none. **Theming**: config keys over a box model (borders, outlines, rounded corners,
  per-text boxes). Themes ship in `themes/`.
- **App launch**: `tofi-drun` prints the Exec line for `swaymsg exec`/xargs, or launches directly
  with `--drun-launch=true` (README).

### wofi
Repo: https://hg.sr.ht/~scoopta/wofi (Mercurial; files fetched raw, last commit 2026-09-27). Tag v1.5.3. GPL-3.0 (`COPYING.md`).

- **Stack** (`meson.build`): C with `gtk+-3.0`, `wayland-client` and `gio-unix-2.0`. It ships the
  wlr-layer-shell, xdg-output and xdg-shell protocol XML itself.
- It does **not** use gtk-layer-shell. `src/wofi.c` takes GTK's `wl_surface`
  (`gdk_wayland_window_set_use_custom_surface`, `gdk_wayland_window_get_wl_surface`) and binds
  `zwlr_layer_shell_v1` itself. Without layer-shell it falls back to a normal window.
- **Plugins**: modes are shared objects loaded with `dlopen` (`wofi.c:1860`). A public C API is
  installed as `wofi-1` headers plus a pkg-config file (`meson.build`). The built-in modes are run,
  drun and dmenu.
- **Theming**: GTK3 CSS.

### rofi (2.0, Wayland mainline)
Repo: https://github.com/davatorium/rofi @ `1ad1df6` (2026-10-01). Release 2.0.0 (2025-09-01). MIT/X11 (`COPYING`).

- 2.0.0 "merged back lbonn his great wayland port of rofi into mainline. Wayland is now an
  officially supported backend" (`releasenotes/2.0.0/release-2.0.0.markdown`). Autotools was dropped.
  Builds can be X11-only, Wayland-only or both, with runtime selection.
- **Stack** (`meson.build`): C with glib, gmodule, cairo, pango/pangocairo, xkbcommon and
  gdk-pixbuf. Wayland protocols: xdg-shell, ext-foreign-toplevel-list, primary-selection,
  keyboard-shortcuts-inhibit, text-input-v3, wlr-foreign-toplevel-management, wlr-layer-shell, and
  cursor-shape when wayland-protocols is 1.32 or newer.
- **Icons** (`source/rofi-icon-fetcher.c`): freedesktop theme lookup via the bundled `libnkutils`
  (`nkutils-xdg-theme.h`), decoded with gdk-pixbuf. Fetching is asynchronous on a
  `g_thread_pool`. drun keeps a desktop-file cache (`drun-use-desktop-cache`).
- **Theming**: the "rasi" language (flex/bison parser), CSS-like, with `linear-gradient(...)`, real
  transparency, per-widget layout and images (`doc/rofi-theme.5.markdown`). This is the deepest
  config-file theming among the CPU-rendered launchers. It has no blur of its own and no shaders.
- **Extension model**: (1) C plugins loaded through gmodule. (2) **Script mode**: an executable
  prints rows on stdout and receives `ROFI_RETV`/`ROFI_DATA` env vars. Rows carry options such as
  `aap\0icon\x1ffolder` and mode options such as `\0prompt\x1f...` (`doc/rofi-script.5.markdown`).
  (3) dmenu mode with the same row options. Fuzzel and others copied this row protocol.

### bemenu
Repo: https://github.com/Cloudef/bemenu @ `44b82f9` (2025-08-29). 0.6.23 (2024-07-19). Client GPL-3.0, library LGPL-3.0.

- C library plus a client. Renderers are separate `.so` files loaded at runtime (`lib/renderers/`:
  curses, x11, wayland). The Wayland renderer links `wayland-client cairo pango pangocairo xkbcommon`
  and uses xdg-shell, wlr-layer-shell, fractional-scale and viewporter (`GNUmakefile`).
- Shows that a menu can be a reusable library with pluggable output backends.

### anyrun
Repo: https://github.com/anyrun-org/anyrun @ `55230c6` (2026-09-29). v26.6.1 (2026-06-09). GPL-3.0.

- **Stack** (`anyrun/Cargo.toml`): Rust, `gtk4 0.10`, `gtk4-layer-shell 0.7`, a relm4 fork, `tokio`,
  `abi_stable`, and config in RON.
- **Process model**: one-shot by default. `anyrun daemon` registers a GApplication on D-Bus, and
  later invocations call into it (`anyrun/src/main.rs`, `DaemonState`). The README notes that
  NVIDIA users may need `GSK_RENDERER=ngl` because of a driver hang on exit.
- **Plugins**: Rust `cdylib` `.so` files with a stable ABI through `abi_stable` ("You only need 4
  functions!", README). Since 25.12.0 plugins are loaded by a separate binary,
  **anyrun-provider** (https://github.com/anyrun-org/anyrun-provider), which talks to the frontend
  over a socket. That repo offers the `anyrun-provider-ipc` crate so other frontends can reuse the
  plugins.
- **Theming**: GTK4 CSS (`style.css`).

### walker (v2, Rust) and elephant (Go backend)
Repos: https://github.com/abenz1267/walker @ `a515085` (2026-10-08), v2.17.2.
https://github.com/abenz1267/elephant @ `a35b3b5` (2026-09-29), v2.22.1. Both GPL-3.0.

- **History**: walker was written in Go through v0.13.26 (2025-08-13). Its `go.mod` at that tag uses
  `diamondburned/gotk4` (GTK4 bindings), `gotk4-layer-shell` and `govips`. v1.0.0 (2025-09-09) is a
  Rust rewrite that moves all data providers into **elephant**, a separate Go daemon. v2.0.0
  (2025-10-05) reworked actions "to allow implementing custom providers".
- **walker stack** (`Cargo.toml`): `gtk4 0.11`, `gtk4-layer-shell 0.8`, `gdk4-wayland`, plus
  `wayland-client`/`wayland-scanner` for extra protocols, `protobuf`, `grass` (a SCSS compiler),
  `nucleo-matcher`, `poppler-rs` (PDF preview) and `niri-ipc`.
- **Blur**: `src/wayland_blur.rs` generates client code for **ext-background-effect-v1** and attaches
  it to GTK's surface. GTK does not do this, so walker added it by hand.
- **Process model** (README): `walker --gapplication-service` keeps it resident. Opening is either
  `walker` (D-Bus activation) or, "for an even faster launch", raw bytes sent to
  `$XDG_RUNTIME_DIR/walker/walker.sock` (`nc -U`), which skips argument parsing. elephant must
  already be running.
- **Theming**: a theme directory containing `style.css` plus a **GtkBuilder `.xml` layout per item
  type** (`item_files.xml`, `item_calc.xml`, `layout.xml`, `preview.xml`, ...). Themes inherit from
  the default theme.
- **elephant** (`go.mod`, README, `internal/providers/load.go`):
  - A Go daemon. Frontends connect to a Unix socket and exchange **protobuf** messages: Query,
    Activate, Menu, Subscribe (`pkg/pb/*.proto`). `QueryResponse.Item` includes identifier, text,
    subtext, icon, provider, score, fuzzy match positions, mimetype, preview, state and actions.
  - Providers are **Go `plugin` .so files** (`go build -buildmode=plugin`), loaded with
    `plugin.Open` and looked up by symbol (`Available`, `Name`, `Activate`, ...). Inference: Go
    plugins require the exact same toolchain and dependency versions as the host binary, which is
    a known fragility of this design.
  - User-defined menus come from TOML, and dynamic menus from **Lua** (`gopher-lua`)
    (`internal/providers/menus/README.md`). Other deps include fzf's matcher library, sqlite and
    fsnotify.
  - Launch prefix auto-detection: `app2unit`, else `uwsm` if `uwsm check is-active` succeeds
    (`pkg/common/runprefix.go`).
  - README (commit 2026-09-29): "**Development is on hold.**"

### sherlock
Repo: https://github.com/Skxxtz/sherlock @ `0bcef09` (main, 2026-01-24). Tag v0.1.15-2. GPL-3.0.

- main branch (`Cargo.toml`): Rust, `gtk4 0.9`, `gtk4-layer-shell 0.4`, tokio, rusqlite, zbus,
  simd-json, bincode.
- Daemon option: with `daemonize = true` the rendered window stays in memory, and the `open`
  message goes to `/tmp/sherlock_daemon.socket`. The docs say this "will consume more memory ... will
  allow faster startup times" (`docs/config.md`).
- A **`gpui` branch** (last commit 2026-04-25) renames the crate to `sherlock-gpui 0.2.0` and depends
  on Zed's `gpui`/`gpui_platform` at a pinned rev, plus `wayland-client` and `wayland-protocols-wlr`.
  It is a rewrite onto a GPU-native Rust UI framework. I did not check which Linux renderer GPUI
  uses.
- Plugins are external repos (`sherlock-wiki`, `sherlock-confetti`, "a shader-based animation
  covering your entire screen").

### vicinae
Repo: https://github.com/vicinaehq/vicinae @ `28ac1ef` (2026-10-09). v0.29.1 (2026-09-30). GPL-3.0. About 10k stars, the most active Raycast-style launcher on Linux.

- **Stack**: C++23 with `find_package(Qt6 6.9 ... Core Qml)`, KF6, glaze (JSON), sqlcipher and
  pugixml. Layer-shell comes from layer-shell-qt, either the system copy or one checked out at
  build time (`CMakeLists.txt`). `AGENTS.md`: "built with QtQuick ... QML for presentation and C++
  for business logic". A C++ "view host" exposes `Q_PROPERTY`/`Q_INVOKABLE` members and QML binds
  to them.
- **UI rewrite**: v0.20.0 (2026-02-26) is "a complete rewrite of the Vicinae UI layer from QWidgets
  to QtQuick (QML). This unlocks ... smooth scrolling, GPU-accelerated rendering, better font
  rendering, and polished animations almost for free" (release notes).
- **Process model**: a resident server (`extra/vicinae.service`: `ExecStart=vicinae server
  --replace`, `PartOf=graphical-session.target`) with a CLI client (`src/cli`). Separate helper
  processes handle the file indexer, the data-control (clipboard) server and the snippet expander
  (`src/`).
- **Protocols** (own code): wlr-layer-shell, wlr-foreign-toplevel-management, xdg-activation
  (`internal/wayland/xdg-activation.cpp`), ext-background-effect-v1
  (`services/window-material/ext-background-effect-v1-manager.cpp`, behind a "window material"
  abstraction with a null backend), and its own `vicinae-hotkey-v1` / `xx-hotkey-v1` drafts.
- **Launch**: if `uwsm check is-active` succeeds it prefixes `uwsm-app --` or `uwsm app --`
  (`utils/environment.hpp`). A user `launchPrefix` overrides this.
- **Extensions**: React/TypeScript, compatible with the Raycast API (`@vicinae/api`,
  `raycast-api-compat`). They run in a **Node `extension-manager` process** spawned by the server,
  which loads extension workers on demand and uses a custom `react-reconciler` (0.32)
  (`src/typescript/README.md`). IPC code is generated by the in-tree `figura` generator. It also
  supports Raycast-style script commands and a dmenu mode (README).
- **Theming**: TOML themes with `[colors.core]`, accents and per-surface colours, inheritance and
  derived colours (`extra/theme-template.toml`), plus a matugen template and a pywal converter.

### hyprlauncher (+ hyprtoolkit)
Repos: https://github.com/hyprwm/hyprlauncher @ `339be04` (2026-09-09), v0.1.6. BSD-3.
https://github.com/hyprwm/hyprtoolkit (BSD-3).

- C++23. Deps: `hyprtoolkit`, `pixman`, `libdrm`, `hyprwire` (IPC), `icu-uc`, `hyprlang`,
  `fontconfig`, `libqalculate`, `xkbcommon` (`CMakeLists.txt`). README: "Daemon by default: instant
  opening of the launcher", multi-threaded fuzzy search, frequency cache, manual (dmenu-like)
  entries. The IPC protocol is in `protocols/hyprlauncher_core.xml`.
- **hyprtoolkit** is "a modern C++ Wayland-native GUI toolkit" aiming for "smooth animations" and
  "simple system theming". It is explicitly not cross-platform. Deps: `OpenGL GLES3`, `egl`, `gbm`,
  `aquamarine`, `hyprgraphics`, `pango`/`cairo`, `xkbcommon`. Protocols: xdg-shell, linux-dmabuf,
  fractional-scale, viewporter, cursor-shape, session-lock, tablet, text-input-v3,
  linux-drm-syncobj, wlr-layer-shell, keyboard-shortcuts-inhibit (its `CMakeLists.txt`).
- Inference: this is the one launcher in the survey built on an in-house GPU toolkit made for
  shell components. Its text path is Pango/cairo rasterised and uploaded, not a GPU glyph atlas
  engine.

### kickoff
Repo: https://github.com/j0ru/kickoff @ `1850491` (2026-04-22). v0.7.6 (2026-01-27). GPL-3.0.

- Rust with `smithay-client-toolkit 0.20`, `fontdue` (pure-Rust rasteriser), the `fontconfig` crate,
  `image` and `fuzzy-matcher`. No toolkit. Draws into shm. Searches `$PATH` plus history. No icons.
  Binds layer-shell only (no fractional-scale or activation code in `src/`).

### yofi
Repo: https://github.com/l4l/yofi @ `a46a329` (2026-04-15). MIT.

- Rust with `sctk 0.18`, **raqote** (a CPU 2D vector library), `fontdue`, fontconfig,
  `freedesktop-icon-lookup`, `resvg 0.40` (SVG icons) and `png`. Modes: apps, bins, dialog (dmenu).
  Has a usage cache. Pins older crates.

### onagre and cosmic-launcher (pop-launcher frontends)
- onagre: https://github.com/onagre-launcher/onagre @ `c68c716` (2025-04-24), 1.1.1, MIT. Uses
  `iced 0.12` with the `wgpu`, `svg` and `image` features, `freedesktop-icons`, and
  `onagre-launcher-toolkit`. Built "on top of pop-launcher" (README).
- cosmic-launcher: https://github.com/pop-os/cosmic-launcher @ `03195dd` (2026-10-07), GPL-3.0.
  libcosmic features include `wayland`, `single-instance`, `desktop-systemd-scope`, `xdg-portal`
  and an optional `wgpu` (`Cargo.toml`).
- **pop-launcher** (https://github.com/pop-os/launcher, MPL-2.0, README): "Desktop launchers may
  interface with this service via spawning the pop-launcher process and communicating to it via
  **JSON IPC over the stdin and stdout pipes**. The launcher service will also spawn plugins found
  in plugin directories on demand". Each plugin is a separate executable described by `plugin.ron`
  (`regex` routing, `isolate`, `persistent`, `history`, `no_sort`). Plain scripts with `# name:` /
  `# icon:` headers work too. Plugins and frontends "may also be written in any language", and a
  crashed plugin is cleaned up without taking down the launcher.

### centerpiece
Repo: https://github.com/friedow/centerpiece @ `1eb676e` (2026-09-22). Last release v1.1.1 (2024-08). MIT.

- Moved **from iced to egui** in PR #254 (2026-02-03). The PR text: "startup time ... has gotten
  slower and slower ... The most pressing issue ... is font loading" (iced/cosmic-text loads every
  system font, tracked in issue #183, which links to iced#2455 and cosmic-text#282). Measured
  startup: "with iced ca. ~550ms, with egui ca. ~190ms".
- PR #262 (2026-04-28) replaced eframe with **`wayapp`**, "a minimal wrapper around egui rendering
  using wgpu" on sctk that supports layer-shell. Window transparency was an open problem in that PR.
- Current deps (`client/Cargo.toml`): `egui 0.35`, `wayapp 0.3.2`, `smithay-client-toolkit 0.20`,
  `smol`/`async-channel` for plugins, `freedesktop-desktop-entry`, `swayipc`. Plugins are in-tree
  Rust modules plus an external indexer service (`services/index-git-repositories`).

### gauntlet
Repo: https://github.com/project-gauntlet/gauntlet @ `ca066da` (2025-10-02). v21 (2025-08-16). MPL-2.0.

- README: "The project is no longer being developed." Kept here as a design reference: Rust with
  an iced 0.13 fork (`wgpu` + `tiny-skia`), `tonic` gRPC, `interprocess` and `sctk 0.19`. Plugins
  are **TypeScript/React with a custom reconciler running on Deno** (sandboxed, with explicit
  permissions). Plugins are distributed as git branches. Wayland support was "best-effort".

### Ulauncher
Repo: https://github.com/Ulauncher/Ulauncher @ `936ec31` (2026-10-11). v6.0.0-beta35 (2026-09-21). GPL-3.0-or-later.

- Python and PyGObject. The UI pins `Gtk 3.0`/`Gdk 3.0` (`ulauncher/ui/__init__.py`). Layer-shell
  goes through `GtkLayerShell 0.1` (`ui/helpers/layer_shell.py`). It runs as a D-Bus-activated
  resident service (`ulauncher.service`, `Type=dbus`).
- **Extensions** (`docs/architecture/extension-ipc.md`): "Extensions run in separate processes ...
  (not sandboxed). They communicate with Ulauncher via Unix socket pairs using a JSON-line
  protocol". The child socket FD is passed through an environment variable.

### Albert
Repo: https://github.com/albertlauncher/albert @ `c9ec162` (2026-09-13). v35.1.0 (2026-07-25).

- C++/Qt6 with QCoro and many C++ plugins plus a Python plugin host (`plugins/`). The frontend
  itself is a plugin (`widgetsboxmodel`). I found no layer-shell code (grep). **Licence**: "Albert
  license v1.1", which permits personal use and has platform restrictions. It is not OSI open
  source, so learn from its docs only.

### Quickshell and the shells built on it

**Quickshell**: https://git.outfoxxed.me/quickshell/quickshell @ `4f508be` (= v0.3.2, 2026-10-08). LGPL-3.0 (with GPL-3.0 text). v0.3.0 was tagged 2026-05-04.

- C++ and Qt6 (minimum 6.6). Shells are written in QML. Its own Wayland modules (`src/wayland/*`,
  protocols via `wl_proto(...)`) cover wlr-layer-shell, xdg-shell, **ext-background-effect-v1**
  (added in v0.3.0: "Added ext-background-effect window blur support"), linux-dmabuf,
  ext-session-lock, wlr-foreign-toplevel, ext-workspace, idle-inhibit/notify and
  keyboard-shortcuts-inhibit, plus Hyprland IPC.
- `DesktopEntries` / `DesktopEntry.execute()` parse desktop files in C++
  (`src/core/desktopentry.cpp`). Icons are resolved through `QIcon::fromTheme`
  (`src/core/iconimageprovider.cpp`). I found no xdg-activation token handling (grep).
- Model: one resident process hosts the whole shell, and panels open and close through
  `qs ipc call ...`. That is why Quickshell launchers open instantly: the window and scene graph
  already exist. QML supports hot reload.

**DankMaterialShell (DMS)**: https://github.com/AvengeMedia/DankMaterialShell @ `002ab65` (2026-10-11). v1.6.3 (2026-10-07). **MIT** (relicensed from GPL-3.0 on 2025-11-12, `quickshell/LICENSE_CHANGE_12_11_2025.md`).

- A split design. QML UI on Quickshell (`quickshell/`), with **a Go backend and CLI** (`core/`,
  `go 1.27.1`): "Command-line interface and daemon for shell management and system control"
  (`core/README.md`). The Go side is a Wayland *client* through the pure-Go `dankgo` library (MIT),
  using gamma-control, screencopy, layer-shell, output-management, ext-data-control and
  ext-workspace. `core/internal/blur/probe.go` checks for `ext_background_effect_manager_v1`.
- Launcher: `Modals/DLauncherV2/` (Controller, Scorer.js, ResultsList, SpotlightLauncherContent),
  opened with `dms ipc call spotlight toggle`. **`dms dmenu`** is a "dmenu-compatible picker backed
  by the DMS app launcher": stdin rows go to the resident QML launcher through a Unix socket, and a
  flock queue serialises concurrent pickers (`core/internal/dmenuipc/`).
- Plugins: QML components with a `plugin.json` that declares `"type": "launcher"` and a trigger
  prefix (`quickshell/PLUGINS/README.md`, "Launcher Plugins"). There is a public registry at
  plugins.danklinux.com.
- Visuals: custom fragment shaders compiled with qsb (`quickshell/Shaders/frag/*.frag`: wallpaper
  transitions such as `wp_iris_bloom`, `wp_portal`, `wp_pixelate`, plus `blob`, `connected_arc`,
  `viz_bars`) and matugen-generated Material 3 palettes.

**caelestia shell**: https://github.com/caelestia-dots/shell @ `21a7537` (2026-10-11), v2.5.0, GPL-3.0. QML plus a C++ Qt plugin
(`plugin/CMakeLists.txt`: Qt6 ShaderTools/Quick/Sql/DBus, libqalculate, pipewire, aubio, cava).
The launcher (`modules/launcher/`) uses an fzf.js-based `Searcher` with weighted keys and prefix
modes, and an app frequency DB. It launches apps through `DesktopEntry.execute()` or a terminal
wrapper (`services/Apps.qml`).

**end-4 dots-hyprland ("ii")**: https://github.com/end-4/dots-hyprland @ `2f2a376` (2026-10-11), GPL-3.0. A QML launcher/overview search
(`dots/.config/quickshell/ii/services/LauncherSearch.qml`, `AppSearch.qml`). It has prefix modes
(apps, actions, clipboard, emoji, math, shell command, web search) and `Quickshell.execDetached`
for actions.

### Small pure-Go launchers (relevant to a Go-centric owner)
- **Gofer**: https://codeberg.org/JakeAtLinux/Gofer @ `fb308ac` (2026-08-28). "written in pure Go.
  Zero C dependencies, single static binary." `go.mod`: `neurlang/wayland`, `jezek/xgb` (X11),
  `golang.org/x/image`, `ebitengine/purego`. libxkbcommon is loaded at runtime with
  `purego.Dlopen("libxkbcommon.so.0")`, so it needs no cgo.
- **wlrun**: https://codeberg.org/vighd/wlrun @ `cfb83c2` (2026-07-07). `go.mod`:
  `rajveermalviya/go-wayland` (upstream last pushed 2023-01-30), `purego`, `x/image`. Binds
  layer-shell, viewporter and fractional-scale. libxkbcommon via purego.
- Go Wayland client libraries seen in this survey: `neurlang/wayland` (MIT, pushed 2026-10-07;
  used by elephant and Gofer), `dankgo` (MIT; DMS), `rajveermalviya/go-wayland` (BSD-2, stale).
  For GTK there is `diamondburned/gotk4` (walker ≤ v0.13). Not investigated: Gio UI (gioui.org). I
  did not verify whether it supports layer-shell.

---

## 3. Terminals as rendering references

### foot
https://codeberg.org/dnkl/foot @ `cb27717` (2026-09-18). 1.28.0 (2026-09-02). MIT.
- C. Deps: `pixman-1`, `wayland-client/cursor`, `xkbcommon >=1.6`, `fontconfig`, `fcft`, and
  `utf8proc` optional (`meson.build`). Protocols: xdg-shell, xdg-decoration, xdg-output,
  primary-selection, **presentation-time**, text-input-v3, xdg-activation, viewporter,
  fractional-scale, cursor-shape, single-pixel-buffer, xdg-toplevel-icon, xdg-system-bell,
  color-management, xdg-toplevel-tag (if available), and **ext-background-effect** (if available).
- **fcft** (https://codeberg.org/dnkl/fcft, 3.3.3 2025-12-27): "font loading and glyph
  rasterization library built on-top of FontConfig, FreeType2 and pixman ... created to support
  foot, for which I found Cairo to be too slow ... first class support for threaded glyph lookup,
  and it aggressively caches rasterized glyphs". HarfBuzz is optional for grapheme and run shaping.
  This is the same text stack fuzzel uses.
- **Server mode** (README): `foot --server` plus `footclient`. The stated reason is "reduced startup
  time - loading fonts and populating the glyph cache takes time, but in server mode it only
  happens once". It supports systemd socket activation. Downside: all windows share one I/O thread,
  and a crash takes down every window.
- README on fractional scaling: true fractional-scale plus viewporter avoids blurry fonts compared
  with the older "render at 2× and downscale" approach.

### kitty
https://github.com/kovidgoyal/kitty @ `7a75129` (2026-10-11). 0.49.2 (2026-10-01). GPL-3.0.
- C core with Python and Go kittens. It carries its own **GLFW fork** (`glfw/`). Requires OpenGL 3.1
  on Linux (`kitty/data-types.h`). FreeType, HarfBuzz and fontconfig are used on Linux (`setup.py`).
  libxkbcommon ≥ 0.5.
- Wayland protocols (`glfw/source-info.json`): xdg-shell, viewporter, fractional-scale,
  cursor-shape, single-pixel-buffer, text-input-v3, xdg-activation, xdg-toplevel-icon/tag,
  system-bell, **ext-background-effect**, **kwin-blur**, **wlr-layer-shell**, idle-inhibit,
  shortcuts-inhibit, and others.
- `--single-instance`: "share a single sprite cache on the GPU and also reduces startup time"
  (`kitty/simple_cli_definitions.py`). The `panel` and `quick-access-terminal` kittens draw
  layer-shell surfaces (docks, wallpaper, quake-style dropdown) with the terminal renderer
  (`docs/kittens/panel.rst`).

### alacritty
https://github.com/alacritty/alacritty @ `29dc553` (2026-10-05). 0.17.0 (2026-04-06). Apache-2.0.
- Rust with `winit 0.30` (on Wayland: smithay-client-toolkit, wayland-client, xkbcommon-dl, per
  Cargo.lock) and `glutin 0.32` (EGL). Text comes from `crossfont 0.8`, which uses freetype-rs and
  yeslogic-fontconfig-sys on Linux (crates.io deps). There is no HarfBuzz shaping (inference: no
  ligatures, consistent with the project's stance). `--daemon` and `alacritty msg create-window`
  provide multi-window from one process.

### ghostty
https://github.com/ghostty-org/ghostty @ `24da727` (2026-10-10). v1.3.1 (2026-03-13). MIT.
- Zig. On Linux the app runtime is GTK4/libadwaita (`src/apprt/gtk`), with `gtk4-layer-shell` used
  for the quick terminal (`winproto/wayland.zig`). Renderer backends are OpenGL and Metal
  (`src/renderer/`), with a `Dmabuf.zig` path. The font backend on Linux is `fontconfig_freetype`
  (`src/font/backend.zig`), with its own shaper/atlas code and bundled freetype, harfbuzz,
  fontconfig, glslang, spirv-cross, simdutf and highway (`pkg/`).
- Own Wayland globals beside GTK: ext-background-effect, KDE decoration/slide/output-order,
  xdg-activation (`winproto/wayland/Globals.zig`).
- `custom-shader`: Shadertoy-compatible GLSL post-processing with `iTime`, `iResolution` and
  `iChannel0` (the terminal frame), plus Ghostty extras such as `iCurrentCursor`, `iPalette[256]`
  and the colours (`src/config/Config.zig`). glslang and spirv-cross translate the shaders per
  backend.
- `gtk-single-instance` defaults to `detect` (single instance unless launched with CLI args or
  from a terminal). Each surface can be placed in a transient systemd scope
  (`app-ghostty-surface-transient-<pid>.scope`, `src/apprt/gtk/cgroup.zig`).

### wezterm
https://github.com/wezterm/wezterm @ `e2a98e1` (2026-10-10). Last tagged release 20240203-110809. MIT.
- Rust with its own `window` crate (on Wayland: smithay-client-toolkit, wayland-egl,
  wayland-protocols-plasma, the `xkbcommon` crate). `front_end` can be `OpenGL` (glium, the
  default), `WebGpu` (wgpu 25) or `Software` (`config/src/frontend.rs`). FreeType, HarfBuzz and
  fontconfig are vendored under `deps/`. Configuration is Lua (`mlua`).

### rio
https://github.com/raphamorim/rio @ `59203cc` (2026-10-03). v0.5.28 (2026-09-17). MIT.
- Rust. Windowing is `rio-window`, a "Winit fork maintained for Rio terminal". The renderer
  "sugarloaf" has contexts for `vulkan.rs` (ash, the **default on Linux**), `metal.rs`, `webgpu.rs`
  (optional wgpu, needed for **librashader**, i.e. RetroArch CRT/scanline filter presets) and
  `cpu.rs` (`sugarloaf/src/context/`, `rio-backend/src/config/renderer.rs`). Fonts use font-kit,
  swash, skrifa, zeno and tiny-skia. GLSL is compiled to SPIR-V at build time with glslc or
  glslang (`sugarloaf/README.md`).
- Inference: rio started on wgpu and moved its default Linux path to native Vulkan, keeping wgpu
  only for the optional filter chain. That suggests wgpu's cost or limits mattered to its author.

---

## 4. Patterns and lessons

### 4.1 What makes a launcher feel instant (verified data points first)

| Evidence | Source |
|---|---|
| tofi: about 2 ms from exec to on-screen, best case, CPU-rendered, one-shot | tofi README |
| tofi: Pango+fontconfig font lookup about 120 ms with 10k fonts; a direct font file path takes under 1 ms | tofi README |
| tofi: what remains is mostly compositor roundtrips and the kernel faulting in shm pages for the first paint (large windows ≈ 20 ms; hugepages help) | tofi `startup_performance.svg`, README |
| centerpiece: iced about 550 ms, egui about 190 ms; the main cost was iced/cosmic-text loading every system font | centerpiece PR #254, issue #183 |
| fuzzel 1.15: shows the window before `.desktop` parsing finishes, loading apps on a thread | fuzzel CHANGELOG |
| foot / kitty: server or single-instance mode avoids repeated font loading and glyph-cache warmup | foot README, kitty CLI docs |
| walker, anyrun, sherlock, Ulauncher, vicinae, hyprlauncher, all Quickshell shells: keep a resident process, and opening becomes an IPC message | respective READMEs and units |
| walker: a raw socket write is faster than a D-Bus-activated `walker` call | walker README |

Patterns (inference from the table):
1. **There are two strategies for instant opening.** (a) A lean one-shot binary: C or Rust, CPU
   rendering into shm, no toolkit, no fontconfig scan on the hot path, precomputed caches
   (tofi, fuzzel, kickoff). (b) A resident process with a pre-built window or scene, where opening
   is a socket or D-Bus message (every GTK4, Qt/QML and Raycast-style launcher). Toolkit-based
   launchers nearly all end up with option (b), because GTK4 or Qt startup plus GPU context
   creation is too slow for one-shot use.
2. **Font discovery is the most-cited single cost.** Fixes in use: skip fontconfig with explicit
   paths (tofi), cache rasterised glyphs and share them across windows (fcft, foot server), avoid
   toolkits that enumerate every system font at startup (centerpiece's reason for leaving iced).
3. **Show first, fill later.** Parse `.desktop` files, icons and providers asynchronously (fuzzel's
   thread, rofi's icon thread pool). Inference: in a resident design this cost moves to daemon
   start, and inotify/fsnotify keeps the data current (walker uses `notify`, elephant uses
   `fsnotify`, Quickshell has `desktopentrymonitor.cpp`).
4. Compositor roundtrips set a floor of a few milliseconds for one-shot clients (tofi timeline). A
   resident client that keeps its layer surface, or recreates it cheaply, avoids part of this.
   Inference: whether hide/show is done by unmapping the layer surface or destroying it was not
   checked per project.

### 4.2 What produces the best-looking results
- **The most visually ambitious launchers all run on retained-mode GPU scene graphs with
  animation: QtQuick/QML** (DMS, caelestia, end-4, vicinae since v0.20) **or GTK4 GSK with CSS**
  (walker, anyrun, sherlock). Vicinae's own release notes credit the QML move with "smooth
  scrolling, GPU-accelerated rendering, better font rendering, and polished animations almost for
  free".
- **Custom shaders** appear in QML shells (DMS `Shaders/frag/*.frag` compiled with qsb; caelestia
  links Qt ShaderTools), in Ghostty (Shadertoy-style post-processing over the whole surface) and in
  rio (librashader CRT presets). Inference: for a grimdark Warhammer-40k look (scanlines, CRT glow,
  noise, parchment or metal textures, animated gradients), the closest existing references are
  Ghostty's `custom-shader` uniforms and rio's librashader pipeline. Both apply a post-process pass
  over a fully rendered frame.
- **Blur behind the window** has two mechanisms: (1) compositor-side rules keyed on the layer
  namespace (inference: Hyprland/niri layer rules, which need nothing from the client), or (2) the
  new **ext-background-effect-v1** protocol, bound in this survey by walker (by hand, beside GTK4),
  vicinae, Quickshell (v0.3.0, 2026-05), foot, kitty and ghostty (DMS probes for it). KDE's
  `org_kde_kwin_blur` is still bound by kitty. None of the CPU-rendered launchers (fuzzel, tofi,
  rofi, bemenu) bind a blur protocol.
- **Config-file theming ceiling**: rofi's rasi is the richest non-toolkit format (CSS-like,
  gradients, per-widget layout). fuzzel and tofi offer colours, borders and radii only. walker adds
  **per-item-type GtkBuilder XML layouts** beside CSS, which is a useful idea for "theme controls
  structure, not just colour".

### 4.3 Recurring stacks
- **C + libwayland + pixman/cairo + fcft or Pango + libxkbcommon** (fuzzel, foot, tofi, rofi,
  bemenu): smallest, fastest startup, the most code you write yourself, limited visual effects.
- **Rust + smithay-client-toolkit + a CPU rasteriser** (kickoff: fontdue; yofi: raqote+fontdue+resvg):
  the same philosophy in Rust. These are smaller projects with simpler text (fontdue has no
  shaping).
- **Rust + GTK4 + gtk4-layer-shell** (anyrun, walker, sherlock-main): CSS theming and a mature
  widget set. Needs a daemon to feel fast. Protocols GTK lacks (ext-background-effect) are bound by
  hand through `gdk4-wayland` and `wayland-client`.
- **Rust + iced / libcosmic / egui + wgpu** (onagre, cosmic-launcher, gauntlet, centerpiece): one
  documented startup problem (centerpiece, 550 ms) and two projects in this group are
  stale or abandoned (onagre, gauntlet). Layer-shell support depends on toolkit forks or wrappers
  (iced_layershell, wayapp, libcosmic).
- **C++/Qt6 QtQuick + layer-shell-qt or Quickshell** (vicinae, all the QML shells): the most
  polished visuals and the fastest-growing ecosystem in 2026 by stars and activity. Resident by
  design.
- **Own GPU toolkit** (hyprtoolkit: EGL/GLES3 + Pango textures; sherlock's GPUI branch): a newer
  trend of purpose-built Wayland GPU UI.
- **Go** is used for **backends and daemons**, not for rendering: elephant, DMS `core` (Wayland
  client work, IPC, `dms dmenu`), walker before v1 via gotk4. Pure-Go rendering front ends exist only
  as small hobby projects (Gofer, wlrun). They use `purego` to dlopen libxkbcommon instead of cgo.
- **Text stacks seen**: fcft (FreeType+fontconfig+pixman, optional HarfBuzz); Pango
  (GTK, rofi, tofi default, hyprtoolkit); FreeType+HarfBuzz directly (kitty, wezterm, ghostty,
  tofi fast path); crossfont (alacritty); fontdue (kickoff, yofi); cosmic-text via iced or libcosmic;
  swash/skrifa/font-kit (rio); Qt's own engine (QML). **Keyboard**: libxkbcommon everywhere, linked,
  dlopen'd (xkbcommon-dl, purego) or reached through a toolkit. No project reimplements xkb.
- **Icons**: either the toolkit's icon theme (GTK IconTheme, `QIcon::fromTheme`), gdk-pixbuf (rofi)
  or a hand-written freedesktop theme walk (fuzzel `icon.c`; yofi via `freedesktop-icon-lookup`;
  onagre via `freedesktop-icons`). SVG libraries in use: nanosvg, librsvg, **resvg** (fuzzel option,
  yofi). PNG via libpng or the `png`/`image` crates.

### 4.4 App launching
- **xdg-activation token passed to the child**: fuzzel (sets `XDG_ACTIVATION_TOKEN` and
  `DESKTOP_STARTUP_ID`), vicinae (`xdg-activation.cpp`). GTK and Qt can do this for their own
  windows. I found no token handling in Quickshell's `DesktopEntry.execute()`.
- **systemd scope / uwsm / app2unit**: elephant auto-detects `app2unit` then `uwsm`. vicinae
  detects uwsm and uses `uwsm-app --`. fuzzel has a generic `launch-prefix`. libcosmic has a
  `desktop-systemd-scope` feature. ghostty puts each surface in a transient
  `app-ghostty-surface-transient-*.scope`.
- **Print instead of launch**: tofi-drun can print the command for `swaymsg exec`, which leaves
  scoping to the caller.

### 4.5 Provider / plugin models (relevant to servitor integration)

| Model | Who | Transport | Language freedom | Isolation |
|---|---|---|---|---|
| Executable prints rows; env vars carry state back | rofi script mode, fuzzel/rofi dmenu (`\0icon\x1f...` row options), pop-launcher scripts, vicinae script commands | stdin/stdout, argv/env | any | process |
| Long-lived plugin processes managed by a broker | pop-launcher (JSON over stdio, `plugin.ron` with regex routing), Ulauncher (JSON lines over socketpair), vicinae (Node extension-manager, Raycast API), gauntlet (Deno) | pipes / Unix socket | any (pop-launcher), Python (Ulauncher), TS/React (vicinae, gauntlet) | process; Deno adds permission sandboxing |
| Separate provider daemon used by thin frontends | elephant (protobuf over a Unix socket; query, activate, subscribe; items carry icon, score, fuzzy positions, actions, preview), anyrun-provider (socket + IPC crate), DMS Go core | Unix socket | frontend any; providers Go `.so` (elephant) or Rust `.so` (anyrun) | daemon process, plugins in-process |
| In-process native plugins | wofi (dlopen C API), rofi (gmodule), bemenu renderers, anyrun (abi_stable cdylib), elephant (Go `plugin`), Albert (C++/Python) | function calls | C ABI or language-locked | none |
| Declarative menus | elephant TOML + Lua menus, DMS launcher plugins (QML + `plugin.json`, trigger prefix) | files | Lua / QML | none |

Observations (inference):
- **Thin frontend plus separate provider service is becoming common**: walker/elephant,
  anyrun/anyrun-provider, cosmic-launcher and onagre on pop-launcher, DMS QML on the Go core. It
  lets the expensive data layer stay resident while the UI is replaceable. elephant's protobuf
  schema (`pkg/pb/query.proto`) is a concrete reference for the fields a launcher item needs. Note
  that elephant is now on hold, and its Go-`plugin` approach ties every provider to the host's
  exact toolchain.
- **Process-per-plugin over a simple wire format** (pop-launcher, Ulauncher, rofi script) is the
  only model in this survey where a Go CLI such as servitor could take part as a peer without
  linking into the launcher. pop-launcher's `plugin.ron` (regex routing, persistent, isolate) and
  rofi's row-option encoding are the two most-copied designs.
- **dmenu compatibility is table stakes**: fuzzel, tofi, wofi, rofi, bemenu, walker (via elephant),
  vicinae, hyprlauncher and DMS (`dms dmenu`, which routes into a resident UI with a queue lock) all
  offer it.

### 4.6 Maintenance signals (as of 2026-10-11)
- Very active: vicinae, Quickshell and the QML shells (DMS, caelestia, end-4), walker, fuzzel, foot,
  kitty, ghostty, rio, Ulauncher (v6 betas), anyrun, rofi.
- Slowing, dormant or stopped: tofi (no commits since 2024-12), bemenu (2025-08), onagre (2025-04),
  gauntlet ("no longer being developed"), elephant ("Development is on hold", 2026-09-29),
  sherlock main (2026-01; work continues on the `gpui` branch).
- Licences: MIT (fuzzel, foot, tofi, rofi, yofi, onagre, centerpiece, DMS, ghostty, wezterm, rio),
  BSD-3 (hyprlauncher, hyprtoolkit), Apache-2.0 (alacritty), MPL-2.0 (gauntlet, pop-launcher),
  LGPL-3.0 (Quickshell, bemenu lib), GPL-3.0 (wofi, anyrun, walker, elephant, sherlock, vicinae,
  kickoff, cosmic-launcher, caelestia, end-4, Ulauncher, kitty, bemenu client), non-OSI (Albert).
  This matters only for what can be read as reference. Logis is clean-room.

---

## Source index

Shallow clones at `~/.cache/logis-research/src/<dir>` (HEAD commit and date at survey time):

| dir | repo | commit | date |
|---|---|---|---|
| fuzzel | codeberg.org/dnkl/fuzzel | 815d438 | 2026-09-25 |
| foot | codeberg.org/dnkl/foot | cb27717 | 2026-09-18 |
| tofi | github.com/philj56/tofi | 1eb6137 | 2024-12-30 |
| wofi | hg.sr.ht/~scoopta/wofi (raw files only) | n/a | 2026-09-27 (log) |
| rofi | github.com/davatorium/rofi | 1ad1df6 | 2026-10-01 |
| bemenu | github.com/Cloudef/bemenu | 44b82f9 | 2025-08-29 |
| anyrun | github.com/anyrun-org/anyrun | 55230c6 | 2026-09-29 |
| walker | github.com/abenz1267/walker | a515085 | 2026-10-08 |
| elephant | github.com/abenz1267/elephant | a35b3b5 | 2026-09-29 |
| sherlock | github.com/Skxxtz/sherlock (main) | 0bcef09 | 2026-01-24 |
| vicinae | github.com/vicinaehq/vicinae | 28ac1ef | 2026-10-09 |
| hyprlauncher | github.com/hyprwm/hyprlauncher | 339be04 | 2026-09-09 |
| kickoff | github.com/j0ru/kickoff | 1850491 | 2026-04-22 |
| yofi | github.com/l4l/yofi | a46a329 | 2026-04-15 |
| onagre | github.com/onagre-launcher/onagre | c68c716 | 2025-04-24 |
| cosmic-launcher | github.com/pop-os/cosmic-launcher | 03195dd | 2026-10-07 |
| centerpiece | github.com/friedow/centerpiece | 1eb676e | 2026-09-22 |
| gauntlet | github.com/project-gauntlet/gauntlet | ca066da | 2025-10-02 |
| ulauncher | github.com/Ulauncher/Ulauncher | 936ec31 | 2026-10-11 |
| albert | github.com/albertlauncher/albert | c9ec162 | 2026-09-13 |
| quickshell | git.outfoxxed.me/quickshell/quickshell | 4f508be (v0.3.2) | 2026-10-08 |
| dms | github.com/AvengeMedia/DankMaterialShell | 002ab65 | 2026-10-11 |
| caelestia | github.com/caelestia-dots/shell | 21a7537 | 2026-10-11 |
| end4 | github.com/end-4/dots-hyprland | 2f2a376 | 2026-10-11 |
| gofer | codeberg.org/JakeAtLinux/Gofer | fb308ac | 2026-08-28 |
| wlrun | codeberg.org/vighd/wlrun | cfb83c2 | 2026-07-07 |
| kitty | github.com/kovidgoyal/kitty | 7a75129 | 2026-10-11 |
| alacritty | github.com/alacritty/alacritty | 29dc553 | 2026-10-05 |
| ghostty | github.com/ghostty-org/ghostty | 24da727 | 2026-10-10 |
| wezterm | github.com/wezterm/wezterm | e2a98e1 | 2026-10-10 |
| rio | github.com/raphamorim/rio | 59203cc | 2026-10-03 |

Also read through the GitHub API, without cloning: pop-os/launcher README, anyrun-org/anyrun-provider README,
hyprwm/hyprtoolkit CMakeLists.txt and README, walker `go.mod` at tag v0.13.26, walker v1.0.0/v2.0.0
release notes, vicinae v0.20.0 release notes, sherlock `gpui` branch Cargo.toml, centerpiece
PRs #254/#262 and issue #183, fcft meson.build and README (Codeberg), crates.io metadata for
`wayapp` and `crossfont`.
