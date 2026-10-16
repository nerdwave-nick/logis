# Changelog

All notable changes to logis are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and logis keeps to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html): the caller contract
(the runes, exit statuses, stdout and the provider protocol; README's "Caller
contract" and logis(1)) is the public interface.

## [Unreleased]

### Added

- **A glitch before the CRT pass** (the Go build; new beside Rust v1): the
  theme's `effects.glitch` breaks the glass up like a failing signal (wobble,
  tracking, colour fringe, torn slices, blocks, flashes, a noise band,
  static, a line pulse), calm at rest and hard in episodes: ambient ones at
  the end of each period, counted from the panel's opening, and bursts when
  the panel opens, on a switch of tab and at random (`bursts`). A bare number
  is its strength; 0 is no pass, and `reduce-motion` turns it off. The
  bundled themes wear the owner's looks. The shader is logis's own
  clean-room `glitch.frag`.
- **A custom shader after the built-in effects** (the Go build; new beside
  Rust v1): a theme's `effects.custom-shader` or the config's
  `custom-shader` (which outranks it) names a GLSL ES 3.00 fragment shader
  shaped as Shadertoy's `mainImage`; logis gives it the frame after the
  built-in effects (`iChannel0`), `iResolution`, `iTime` (seconds since the
  panel opened, frozen by `reduce-motion`), `iFrame` and `iGlitch`. A shader
  that cannot be read or compiled is skipped with one line in the log;
  `--check-config` compiles it and reports errors at the shader's own lines.
  The unknown-key error of the config now lists `custom-shader` too.

- **Plain logis is a tabbed launcher where servitor is** (the Go build; a
  divergence from Rust v1): with `servitor` on its PATH, plain `logis` shows
  the applications and servitor mode's rites as two tabs under a strip of two
  plates (Applications left, Rites right, fixed in place, each counting the
  query's matches). Tab, Shift+Tab or a click on a plate switches tabs and
  keeps the query; Return acts in the shown tab (exit 0 for a launched app or
  an invoked rite). It opens on the tab last used, kept in
  `$XDG_STATE_HOME/logis/tab`. `--apps` and `--servitor` are unchanged (Tab
  still selects the next item). The theme gains `tab.radius`; the plates are
  covered by the CRT and glitch passes, and a switch fires the tab burst.

### Changed

- **logis is written in Go** (ADR 0003, ADR 0004): the Go build replaces Rust
  logis v1 on `main`; the Rust code stays in the history (up to 2ffdd02). The
  caller contract (runes, exit statuses, stdout, the provider protocol) is
  v1's, argument by argument, except where an entry here says otherwise.
  Building needs Go 1.27 and a C compiler instead of Rust. What a user sees
  differ from v1, besides the tabbed launcher, the row fit, the glitch and the
  custom shader listed in their own entries:
  - **Rendering** is OpenGL ES 3 through EGL (libglvnd) with the EGL Wayland
    platform, on whichever vendor driver the system has; Vulkan and wgpu are
    gone, and software rendering is Mesa's llvmpipe
    (`LIBGL_ALWAYS_SOFTWARE=1`). The package depends on `opengl-driver`
    instead of `mesa`, and the binary links only libwayland-client,
    libwayland-egl, libEGL, libGLESv2, libfontconfig and libfreetype (no
    libxkbcommon: the keyboard is logis's own XKB).
  - **Text** is shaped by logis's own OpenType engine and hinted by the
    system's FreeType, so glyphs can differ from v1's in their antialiasing.
    In a matched row only the matched letters take the match face (v1 set the
    whole ASCII word in it), and a right-to-left line that starts with digits
    keeps them (v1 dropped that run).
  - **Matching** fixes nucleo-matcher's defects that v1 inherited: every
    Latin letter whose decomposition is an ASCII letter and marks folds (v1's
    tables beyond U+036F were misgenerated and never folded upper-case Latin
    Extended letters); a substring term (`'word`, `!word`) in non-ASCII text
    is also found in its last possible place, scored by the character before
    it, and ignores case when it starts with non-letters; an escaped space in
    a non-ASCII word loses its backslash. Rows with non-ASCII characters can
    therefore match or sort differently.
  - **The CRT pass** is logis's own shader with a group of settings per
    effect (scanlines, glow, aberration, mask, roundness, grain, flicker); a
    bare number is still the group's strength and v1's `bloom`, `curvature`
    and `vignette` still set their groups, so v1 themes stay valid and look as
    they did. Curved glass bends the whole panel, border and glow included,
    and leaves what the bend uncovers transparent.
  - **`reduce-motion`** also stills the rolling scanlines, the wobbling colour
    split and the changing grain.
  - **The daemon's socket protocol** is version 4 (v1 spoke 3): a v1 daemon
    left running refuses the new clients with its reason, exit 2. Restart
    the daemon once after upgrading (`systemctl --user restart logis.service`
    where the units are enabled; otherwise end the old `logis daemon`).
- **The panel's height follows its rows** (the Go build; a divergence from
  Rust v1): the theme's `window.rows` (now eleven in the bundled themes) or
  `--lines` rows at `row.height`, the prompt line, rule and row gaps, exactly,
  so no empty band stays below the last row. On an output too short for them
  the panel shows the rows that fit, spread over the output's height (via
  xdg-output and the output the panel lands on). `window.height` is
  deprecated: still accepted and checked, so v1 themes load, but ignored.

### Removed

- **`LOGIS_GPU_BACKEND`** (the Go build has one backend, EGL with OpenGL ES 3;
  `LIBGL_ALWAYS_SOFTWARE=1` selects llvmpipe) and the Rust build (`cargo`).

## [1.0.0] - unreleased

The first release: everything of the v1 specification. Not yet tagged; the tag
`v1.0.0` waits for the owner's go-ahead.

### Added

- **One binary, two roles.** A resident daemon (`logis daemon`) holds the
  Wayland connection, the GPU, the fonts and the app index warm on
  `$XDG_RUNTIME_DIR/logis.sock`; every other invocation is a thin client that
  starts the daemon when none answers. Sessions: dmenu clients queue,
  interactive sessions (apps, servitor, providers) replace the visible one, and
  a client that leaves cancels its session. `--idle-timeout` (and the config's
  `idle-timeout`) lets an idle daemon exit; it is off by default.
- **dmenu mode** (`logis --dmenu`, `--dmenu0`): a drop-in for
  `fuzzel --dmenu` with fuzzel's runes (`--prompt`, `--prompt-only`,
  `--placeholder`, `--password`, `--lines`, `--width`, `--counter`, `--search`,
  `--select`, `--select-index`, `--only-match`, `--no-run-if-empty`, `--index`,
  `--with-nth`, `--accept-nth`, `--match-nth`, `--nth-delimiter`), streaming
  input (the window opens before stdin ends; 100 000 lines stay responsive),
  the original bytes on stdout, and exit statuses 0 (chosen), 1 (dismissed) and
  2 (error).
- **App launcher** (plain `logis`, or `--apps`): desktop entries from the XDG
  data directories with the visibility rules (`NoDisplay`, `Hidden`,
  `OnlyShowIn`/`NotShowIn`, `TryExec`), icons from the icon theme (SVG and PNG),
  frecency in `$XDG_STATE_HOME/logis/`, desktop actions on Shift+Return,
  terminal apps through the configured terminal, and launches with an
  xdg-activation token in a transient systemd scope and the session's
  environment. `DBusActivatable` entries are started over D-Bus.
- **servitor mode** (`logis --servitor`): every rite of
  `servitor census --vigil` at once, pending aspects shown until they settle,
  then an aspect and the rite's inscriptions, and `servitor invoke` started
  detached; logis closes at once and never waits for the invocation.
- **Providers** (`logis --provider PROGRAM [ARG]...`): the provider protocol,
  version 1 (JSON lines; docs/provider-protocol.md), with pending items,
  badges, multi-stage flows and toasts.
- **The look**: logis's own wgpu renderer (Vulkan, then GL, then Mesa's
  lavapipe or llvmpipe) with SDF shapes, glyphs through glyphon and
  fontconfig, an icon atlas and a post-processing pass (scanlines, grain,
  flicker, vignette, bloom, curvature, chromatic aberration); crisp at
  fractional scales; blur behind where the compositor offers
  ext-background-effect.
- **Themes**: forge crimson & brass (the default) and cogitator phosphor,
  compiled in; user themes in `~/.config/logis/themes/<name>.jsonc` that may
  extend another theme, edited live, with a published JSON Schema
  (`schema/theme.schema.json`). A global `reduce-motion` stills every
  continuous effect.
- **Configuration**: JSONC at `$XDG_CONFIG_HOME/logis/logis.jsonc` with a JSON
  Schema (`schema/logis.schema.json`), `--config`, `--check-config`, and live
  reload that keeps the last sound state and reports errors by file, line and
  column.
- **Input**: libxkbcommon driven directly (Compose, key repeat, consumed
  modifiers), fzf-like matching that ignores case and accents with highlighted
  matches, grapheme-aware editing, the pointer (hover, click, scroll) and paste
  from the clipboard and the primary selection.
- **Motion**: open and close transitions and a gliding selection, paced by the
  compositor's frame callbacks; an idle window draws nothing.
- **Packaging**: a release profile (fat LTO, one codegen unit, stripped), the
  man page logis(1), bash, zsh and fish completions, systemd user units
  (`logis.socket`, `logis.service` with `KillMode=process`; shipped, not
  enabled), and a PKGBUILD in `packaging/arch/` (no AUR package).
- **Aligned columns**: list columns take their width from the rows, up to a
  maximum and with an ellipsis, instead of starting at the next tab stop
  (1b82eff9).

### Fixed

- **Warm opens no longer wait on the compositor before they are shown**
  (d49bceee). Each window asked the GPU driver again what its surface offers;
  Mesa's Wayland WSI answers that with about 30 round trips to the compositor
  per open, so any compositor delay held the window back. The offer is now
  negotiated once, with the daemon's first window. The ~2 s outliers seen in
  the latency harness came from the nested test niri itself: a nested niri the
  session does not draw gets frame callbacks about once a second and stops
  answering meanwhile. The latency tests now run one at a time (the measured
  window is the newest, in view), a watchdog on the test compositor names its
  stalls, and with enforced budgets no warm open may take over 100 ms.

[Unreleased]: https://github.com/nerdwave-nick/logis/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/nerdwave-nick/logis/releases/tag/v1.0.0
