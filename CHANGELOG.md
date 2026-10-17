# Changelog

All notable changes to logis are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and logis keeps to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html): the caller contract
(the runes, exit statuses, stdout and the provider protocol; README's "Caller
contract" and logis(1)) is the public interface. Before 1.0.0 a minor version
may still change it; each such change is listed here.

## [Unreleased]

### Changed

- **A terminal app finds an installed terminal.** Without the config's
  `terminal` or the caller's `$TERMINAL` (a keybind or a socket-started daemon
  rarely has one), logis runs it through `xdg-terminal-exec` on the caller's
  `PATH`, else in the first of foot, kitty, alacritty, wezterm, ghostty,
  konsole, gnome-terminal, xfce4-terminal and xterm found there, each given
  the command its own way (`alacritty -e`, `wezterm start --`, …). With none,
  the error names the app and says to set `"terminal"` in the config file the
  daemon reads, with an example, in the toast and in the daemon's log; it told
  only that none was named before, cut off in the toast.

- **An operating-system error reads as its description alone**, without the
  "(os error N)" the port from Rust left behind: "cannot reach
  /run/user/1000/wayland-nowhere: No such file or directory", and the same
  for a custom shader or theme file that cannot be read.

### Fixed

- **A dmenu switch given a value is refused**, as logis(1) promises ("a value a
  rune cannot bear is an error naming it") and as fuzzel does:
  `--counter=x`, `--index=x`, `--only-match=x` and `--no-run-if-empty=x` are
  unknown runes, exit 2, like `--dmenu=x` and every other switch. They were
  taken as the bare switch before.
- **Matching ignores case by Unicode's case folding throughout**: ΐ (U+1FD3)
  and U+0390, ΰ (U+1FE3) and U+03B0, and the ligatures ﬅ (U+FB05) and ﬆ
  (U+FB06) match each other, which they did not, and a lower-case letter
  that folds to another (ς, ſ) matches it also in a word that types an
  accent.
- **`--select=TEXT` is equal ignoring case by Unicode's case folding**, like
  the matcher: `--select=ΟΔΟΣ` finds "οδος" (final sigma), and ﬅ finds ﬆ. It
  compared lower cases before, which keep those apart.
- **A cut column never draws past its width.** A line is laid out and measured
  in the whole-pixel advances it is drawn with, so a cut line's glyphs and its
  `…` stay within the column (a mixed Hebrew/Arabic line drew 222 px in a
  220 px column), a right-to-left line starts exactly at its right edge, and
  columns fitted to their widest text are as wide as that text is drawn (a few
  pixels wider than before at some scales). A column narrower than `…` shows
  nothing; the ellipsis was drawn outside it. The `…` follows the last visible
  character directly: spaces and tabs where the cut falls are dropped
  ("the rite of the long…", not "the rite of the long …").
- **Tab stops are exact**: stops are 8 drawn spaces apart, and the text after a
  tab starts exactly at its stop, where it could drift a pixel or two.

## [0.1.1] - 2026-10-16

The first release: a grimdark Wayland launcher and dmenu-compatible picker
for wlr-layer-shell compositors such as niri and sway, written in Go.

### Added

- **One binary, two roles.** A resident daemon (`logis daemon`) holds the
  Wayland connection, the GPU, the fonts and the app index warm on
  `$XDG_RUNTIME_DIR/logis.sock`; every other invocation is a thin client that
  starts the daemon when none answers. Sessions: dmenu clients queue,
  interactive sessions (apps, servitor, providers) replace the visible one, and
  a client that leaves cancels its session. `--idle-timeout` (and the config's
  `idle-timeout`) lets an idle daemon exit; it is off by default. Client and
  daemon speak socket protocol version 5: a daemon of another version refuses
  a client with its reason, exit 2.
- **A daemon left running across an upgrade says so**: once its binary was
  replaced since it started, it keeps serving but every panel it shows
  carries the notice "logis updated — …", naming
  `systemctl --user restart logis` when systemd passed it the socket and
  `restart the daemon: kill PID` otherwise; every caller prints the same line
  on stderr (exit status and stdout unchanged), and the daemon's log has it
  once. It never restarts itself.
- **dmenu mode** (`logis --dmenu`, `--dmenu0`): a drop-in for
  `fuzzel --dmenu` with fuzzel's runes (`--prompt`, `--prompt-only`,
  `--placeholder`, `--password`, `--lines`, `--width`, `--counter`, `--search`,
  `--select`, `--select-index`, `--only-match`, `--no-run-if-empty`, `--index`,
  `--with-nth`, `--accept-nth`, `--match-nth`, `--nth-delimiter`), streaming
  input (the window opens before stdin ends; 100 000 lines stay responsive),
  the original bytes on stdout, and exit statuses 0 (chosen), 1 (dismissed) and
  2 (error).
- **A tabbed launcher** (plain `logis`): the applications and servitor mode's
  rites as two tabs under a strip of two plates (Applications left, Rites
  right, fixed in place, each counting the query's matches). Tab, Shift+Tab or
  a click on a plate switches tabs and keeps the query; Return acts in the
  shown tab (exit 0 for a launched app or an invoked rite). It opens on the tab
  last used, kept in `$XDG_STATE_HOME/logis/tab`. Without `servitor` on its
  PATH (a compositor keybind's PATH can differ from the shell's) the rites are
  silent: their plate counts "—", their tab says "cannot find servitor — the
  rites are silent" and that a keybind's PATH can differ from the shell's, and
  Return there does nothing.
- **App launcher** (`logis --apps`): desktop entries from the XDG data
  directories with the visibility rules (`NoDisplay`, `Hidden`,
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
- **The look**: logis's own GPU renderer on OpenGL ES 3 through EGL
  (libglvnd) and the EGL Wayland platform, on whichever vendor driver the
  system has (`LIBGL_ALWAYS_SOFTWARE=1` selects Mesa's llvmpipe), with SDF
  shapes, glyphs and icons from atlases, crisp at fractional scales, and blur
  behind where the compositor offers ext-background-effect.
  - **Text** is shaped by logis's own OpenType engine (bidi, Hebrew and
    Arabic joining, emoji sequences), hinted and rasterised by the system's
    FreeType, with families and fallbacks resolved by fontconfig in-process.
  - **The CRT pass** has a group of settings per effect (scanlines, glow,
    aberration, mask, roundness, grain, flicker); a bare number is the group's
    strength, and `bloom`, `curvature` and `vignette` are accepted as older
    names of `glow.strength`, `roundness.curvature` and `roundness.vignette`.
    Curved glass bends the whole panel, border and glow included, and leaves
    what the bend uncovers transparent.
  - **A glitch before the CRT pass**: the theme's `effects.glitch` breaks the
    glass up like a failing signal (wobble, tracking, colour fringe, torn
    slices, blocks, flashes, a noise band, static, a line pulse), calm at rest
    and hard in episodes: ambient ones at the end of each period, counted from
    the panel's opening, and bursts when the panel opens, on a switch of tab,
    on a key press (`key-ms`, `key-strength`; a held key's repeats and a bare
    modifier do not fire it) and at random (`bursts`). A bare number is its
    strength; 0 is no pass.
  - **A custom shader after the built-in effects**: a theme's
    `effects.custom-shader` or the config's `custom-shader` (which outranks
    it) names a GLSL ES 3.00 fragment shader shaped as Shadertoy's
    `mainImage`; logis gives it the frame after the built-in effects
    (`iChannel0`), `iResolution`, `iTime` (seconds since the panel opened),
    `iFrame` and `iGlitch`. A shader that cannot be read or compiled is
    skipped with one line in the log; `--check-config` compiles it and reports
    errors at the shader's own lines.
- **The panel's height follows its rows**: the theme's `window.rows` (eleven
  in the bundled themes) or `--lines` rows at `row.height`, the prompt line,
  rule and row gaps, exactly. On an output too short for them the panel shows
  the rows that fit, spread over the output's height. `window.height` is
  accepted and checked but ignored. A toast hides the row it covers.
- **Aligned columns**: list columns take their width from the rows, up to a
  maximum and with an ellipsis, instead of starting at the next tab stop.
- **Themes**: forge crimson & brass (the default) and cogitator phosphor,
  compiled in; user themes in `~/.config/logis/themes/<name>.jsonc` that may
  extend another theme, edited live, with a published JSON Schema
  (`schema/theme.schema.json`). A global `reduce-motion` stills every
  continuous effect (the rolling scanlines, the wobbling colour split, the
  changing grain, the glitch and a custom shader's time).
- **Configuration**: JSONC at `$XDG_CONFIG_HOME/logis/logis.jsonc` with a JSON
  Schema (`schema/logis.schema.json`), `--config`, `--check-config`, and live
  reload that keeps the last sound state and reports errors by file, line and
  column.
- **Input**: logis's own XKB keyboard (Compose, key repeat, consumed
  modifiers, keymaps replaced mid-session); fzf-like matching on
  nucleo-matcher 0.3.1's scoring that ignores case and accents (every Latin
  letter whose decomposition is an ASCII letter and marks folds) with
  highlighted matches; grapheme-aware editing; the pointer (hover, click,
  scroll); paste from the clipboard and the primary selection.
- **Motion**: open and close transitions and a gliding selection, paced by the
  compositor's frame callbacks; an idle window draws nothing. Warm opens wait
  on no compositor round trip before they are shown.
- **Packaging**: the man page logis(1), bash, zsh and fish completions,
  systemd user units (`logis.socket`, `logis.service` with `KillMode=process`;
  shipped, not enabled), and a PKGBUILD in `packaging/arch/` (no AUR package)
  that depends on `opengl-driver`. The binary links only libwayland-client,
  libwayland-egl, libEGL, libGLESv2, libfontconfig and libfreetype.
- **Versions from git tags**: a release is the tag `vX.Y.Z`. `logis
  --version` reports the version Go records from the tags (or the one a
  build stamps through `-ldflags -X`), and the package's version, binary and
  man page all take theirs from `git describe` of the commit they build; no
  file but this changelog names a number.

[Unreleased]: https://github.com/nerdwave-nick/logis/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/nerdwave-nick/logis/releases/tag/v0.1.1
