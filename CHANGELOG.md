# Changelog

All notable changes to logis are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and logis keeps to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html): the caller contract
(the runes, exit statuses, stdout and the provider protocol; README's "Caller
contract" and logis(1)) is the public interface.

## [Unreleased]

Pending integration before v1.0.0 is tagged:

- Warm opens no longer stall: a few of every twenty took about two seconds
  before the daemon showed the session (d49bceee).
- List columns are aligned: each column's width comes from the rows, with a
  maximum and an ellipsis, instead of starting at the next tab stop (1b82eff9).

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

[Unreleased]: https://github.com/nerdwave-nick/logis/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/nerdwave-nick/logis/releases/tag/v1.0.0
