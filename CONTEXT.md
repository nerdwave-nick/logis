# logis

logis is a clean-room Wayland launcher and picker that looks like a cogitator of
the Machine God. It serves scripts as a drop-in `dmenu`, launches desktop
applications, and drives servitor through its CLI. This file records the words the
code, the documentation and the tracker use, and what each one means. The shared
specification is the description of the lit project `feat/logis`; the decisions
behind it are in `docs/adr/`. The Go rewrite on branch `go` follows the lit project
`feat/logis-go` and ADR 0003; its own words are under "The Go rewrite" below.

Flag names, code identifiers and protocol fields use the plain terms below. The
grimdark flavour lives only in help, error and UI text (spec §4.6).

## Process and sessions

**logis**:
The single binary. Every invocation is either the daemon or a client.

**Daemon**:
The resident `logis` process. It owns the Wayland connection, the GPU device
(created at start, not lazily), font and glyph caches, the app index and the
providers, and shows and hides layer surfaces per session. It stays resident unless
the optional `idle-timeout` setting (off by default) lets it exit. See ADR 0002.

**Client**:
Every user-facing invocation (`--dmenu`, app mode, servitor mode). It connects to
the daemon's socket, starting the daemon itself if none answers, and waits for its
session's result.

**Daemon socket**:
`$XDG_RUNTIME_DIR/logis.sock`, a framed Unix stream between client and daemon. An
optional systemd user socket unit may start the daemon on first connect.

**Session**:
One client's request to show something and get an answer back: opened by the
client, completed with a result or a dismissal, and cancelled when the client
disconnects. The result goes only to the client that opened the session.

**dmenu session / interactive session**:
A session opened by `--dmenu` (or `--dmenu0`), or by the app or servitor mode. When
a session is visible and another client opens one, dmenu sessions **queue** (each
caller gets its answer in order) and interactive sessions **replace** the visible
one.

**Mode**:
What a session shows and how its result is used: **dmenu mode** (lines from stdin,
the choice printed to stdout), **app mode** (desktop applications, launched by the
daemon) and **servitor mode** (rites and aspects, invoked through servitor's CLI).

**Dismissal**:
Ending a session without a choice (Escape, Ctrl+G, Ctrl+[, or focus loss by
default). In dmenu mode it prints nothing and exits 1.

**Exit status**:
0 with the choice on stdout; 1 on dismissal with empty stdout; 2 on error (no
Wayland session, no layer-shell, invalid configuration, daemon could not start …)
with the message on stderr.

## Items and providers

**Provider**:
Whatever produces the items a session shows: the built-in dmenu provider (stdin),
the apps provider, the servitor provider (logis itself run as a provider process,
`logis provide servitor`, for `logis --servitor`), and external providers
(`logis --provider PROGRAM`). External providers are processes speaking streaming
JSON lines over their stdin and stdout ([provider protocol](docs/provider-protocol.md));
there are no plugin ABIs. A provider learns the user's choice as an **activation**
(an item) or a **submission** (typed text).

**Item**:
One selectable row. It carries a stable id, its columns, an icon or glyph, badges
and an optional pending state. In dmenu mode an item keeps its original input bytes
separately from its display text, so the bytes printed back are exactly the bytes
read.

**Column**:
One field of an item. In dmenu mode, columns are split by the nth delimiter (tab by
default) and chosen with `--with-nth` (display), `--match-nth` (matching) and
`--accept-nth` (output). A shown item's columns are its tab-separated fields; they
line up across rows: each is as wide as the widest text **measured** in it in a row
shown so far (its widths only grow while a list is open), capped by the theme, and
the last takes the rest of the row.

**Query**:
The text typed into the input line. It filters the items with fuzzy, case- and
accent-insensitive matching; matched characters are highlighted. It is edited at a
cursor that always sits between graphemes, so Backspace deletes what a reader sees
as one character.

**Typed text**:
The query itself as a dmenu answer: Return when no item matches prints what was
typed (fuzzel's "free text"). `--only-match` forbids it.

**Preselection**:
The item selected when it arrives (`--select`, `--select-index`), until the user
moves the selection or edits the query.

**Shortcut**:
A key bound to an action (dismiss, accept, select, edit). It is matched with
**consumed-modifier** semantics: only the modifiers the key did not need to produce
its keysym count, so Shift+Tab, Ctrl+[ on any layout and Ctrl+N under Caps Lock all
mean what they say.

**Compose sequence**:
Keys that together type one character (Menu o " → ö), from the locale's Compose
table or the user's (`XCOMPOSEFILE`, `~/.XCompose`). Modifier keys never cancel one.

**Key repeat**:
A held key typed again at the compositor's rate after its delay; the client repeats
it, and a rate of 0 means no repeat.

**Focus loss**:
The visible window losing keyboard focus. It dismisses the session by default; the
config's `"focus-loss": "keep"` leaves it open.

**Hover**:
The pointer moving over a row selects it. A window that opens under a pointer at
rest does not: only a pointer that moves points.

**Paste**:
Inserting a selection's text at the query's cursor: the **clipboard** (Ctrl+V,
Shift+Insert) or the **primary selection** (the middle button). Only the first
non-empty line is inserted, since the query is one line.

**Preedit**:
Text an input method is still composing (text-input-v3, after v1): shown at the
cursor, never part of the query and never matched.

**Stage**:
One list within a multi-stage flow in a single window, such as rite → aspect →
inscriptions, or app → desktop action. The first stage opens and sizes the
window; a provider's next stage replaces the list in it. Items are named by a
stable id within their stage.

**Action**:
Something a provider performs for a chosen item (launching an app, invoking a rite).
Its outcome may be reported as a toast.

**Toast**:
A short, themed notice of an action's outcome or of an error, such as a
provider's result or a configuration error after a live reload. It shows over the
bottom of the list for 3 s (or as long as its provider asks); when the provider
ends its flow meanwhile, the window **lingers** on it without the keyboard.
(servitor mode shows no outcome: servitor announces its own verdicts; its toasts
only tell what logis itself could not do.)

**Pending**:
The state of an item whose data has not settled yet, such as an aspect still
awaiting its auspex. A pending item is shown in a themed pending state (dimmed,
marked "divining…"), never preselected and never activated: Return on it does
nothing.

## Apps

**Desktop entry**:
A `.desktop` file from the `applications` directories under `$XDG_DATA_HOME` and
`$XDG_DATA_DIRS`. logis parses and launches them with its own implementation.

**Desktop-file ID**:
A desktop entry's name across directories: its path below `applications` with
`/` turned into `-`. The first directory that has an ID wins (`$XDG_DATA_HOME`
before `$XDG_DATA_DIRS`); without `.desktop` it is the **application ID** (the
D-Bus name of a `DBusActivatable` app, the app_id of its window).

**App index**:
The desktop entries the launcher shows, read by the daemon and kept resident;
read again when the `applications` directories change.

**Desktop action**:
An additional entry point declared by a desktop entry, offered as a second stage
(Shift+Return on the app).

**Session environment**:
The environment a launched app runs in: the systemd user manager's (what the
session imported), else the environment of the client that asked; never the
daemon's own, which is frozen at its start.

**Activation token**:
An xdg-activation token the compositor issues for the window and the key that
chose an app; passed to the app (`XDG_ACTIVATION_TOKEN`, `DESKTOP_STARTUP_ID`)
so that it opens focused.

**App scope**:
The transient systemd user scope a launched app runs in
(`app-logis-<application ID>-<pid>.scope`), apart from logis, where a user
manager answers.

**Icon theme**:
A freedesktop icon theme (`<base dir>/<name>/index.theme`), looked up for an item's
icon by name: the configured theme, the themes it inherits, then hicolor, then the
pixmaps directories. Each icon is rasterised once per pixel size into the
renderer's **image atlas**, the GPU texture all images are drawn from.

**Frecency**:
The ranking weight that combines how often and how recently an app was chosen,
persisted in `$XDG_STATE_HOME/logis/frecency.json`. It orders the app list and
raises an app's match score by a bounded **boost**.

## servitor

servitor's own `CONTEXT.md` is authoritative for these words; logis uses them as
servitor defines them and talks to servitor only through its CLI.

**Rite**: a named liturgy that brings the machine into one of its aspects.

**Aspect**: one of the conditions a rite can bring the machine into. The current
aspect is marked once known.

**Auspex**: a rite's own scan that reports its current aspect; until it settles,
the rite's aspects are pending.

**Inscription**: a named value carried by one invocation, declared by the rite with
whether it is mandatory, its purpose and its decreed default.

**Invocation**: one performance of a rite into an aspect. Dismissing logis never
aborts an invocation already running.

**Verdict**: how an invocation ended: *triumph*, *reverted* or *faltered*, with the
**verse** (step) it fell at and the **heresy** (error) that felled it.

**Census / augury**: servitor's rituals that list every rite with its aspect, and
read one rite's aspect, inscriptions and omens.

## Wayland and rendering

**Compositor**:
The Wayland server logis runs under. The target is niri; others that offer the
required protocols are best effort.

**Layer shell**:
The `wlr-layer-shell` protocol (`zwlr_layer_shell_v1`), which logis needs to place
its window on the overlay layer with exclusive keyboard focus. A compositor without
it is an error (exit 2), never a silent fallback.

**Layer surface**:
The window logis shows for a visible session, on the overlay layer, namespace
`logis`.

**Renderer**:
logis's own small GPU renderer on wgpu: SDF primitives, glyph quads, an icon atlas
and an optional post-processing pass. There is no CPU renderer. See ADR 0001. The Go
build draws the same on EGL + OpenGL ES 3 (ADR 0003).

**Theme**:
A named look in `themes/<name>.jsonc` (beside the config), selected by
`"theme": "<name>"`, described by `schema/theme.schema.json`. Bundled themes are
compiled in; a user file overrides a bundled theme of the same name. A theme may
**extend** one other theme by name (one level, never in a circle); whatever it
leaves out comes from the bundled default. An invalid theme leaves the **last
sound theme** in force and is reported in the log and as a toast.

**Palette / element / effect**:
The theme's three layers: the **palette** of named colours with alpha; the
**elements** (window, glow, border, panel, decorations, prompt, query, counter,
rule, row, selection, match, badge, pending), which name palette colours and font
**roles**; and the **effects** of the post-processing pass over the body
(scanlines, grain, flicker, vignette, bloom, curvature, chromatic aberration),
each a strength where 0 is off, with the motion timings.

**Continuous effect**:
What moves on its own while a window is visible: the glow's **pulse**, the
**travelling highlight** round the border, the **flicker**, and a pending row's
**shimmer**. They keep the window drawing; **reduce motion** (the config's
`reduce-motion`) stills them all.

**Sigil**:
The image before the prompt's label: logis's own **gear**, or a personal theme's
SVG or PNG file. Original motifs only in the bundled themes.

**Config**:
`logis.jsonc` in `$XDG_CONFIG_HOME/logis/` (or `--config PATH`): JSONC described by
`schema/logis.schema.json`. The daemon applies a sound edit at once; an invalid edit
leaves the **last sound config** in force and is reported in the log and as a toast.

**Motion**:
The window's movement (spec §4.3, v1): the **open transition** (the panel rises
and fades in from its first frame on), the **close transition** (it sinks and
fades out after its session is answered, holding neither keyboard nor pointer),
and the **selection glide** (the band moves to the newly selected row). Frame
callbacks pace it; once it has **settled**, nothing is drawn until the next
change. Its durations, poses and easing curves are the `Motion` parameters.

**Blur behind**:
The compositor blurring what lies behind the panel's body (ext-background-effect),
the config's `blur`; where the compositor does not offer it, there is none.

**Forge crimson & brass / cogitator phosphor**:
The two bundled themes of v1 (`forge-crimson-brass`, the default, and
`cogitator-phosphor`): the glowing brass-to-crimson panel of the research spike,
and a green CRT with curved glass, bloom and an inverse selection.

## The Go rewrite

The words of the Go build on branch `go` (lit project `feat/logis-go`, ADR 0003).
Everything above keeps its meaning there.

**Rust reference**:
The Rust logis on `main` (2ffdd02 at the branch point). Its behaviour, tests,
goldens, fixtures and documentation are what the Go build must match; Go work
never edits it.

**Go branch**:
The long-lived branch `go`, branched from `main`, where Go implementation tickets
integrate. One Go module at the repository root (`github.com/nerdwave-nick/logis`),
the binary in `cmd/logis`, packages under `internal/`.

**Switch-over**:
The single merge that replaces the Rust code on `main` with the Go build, once it
passes the parity bar: every E2E scenario carried over from `feat/logis` and the
performance budgets under the latency harness on a quiet machine.

**x package**:
A package from a `golang.org/x` module, the only Go code logis uses beyond the
standard library. Each is justified by the ticket that introduces it.

**cgo set**:
The C libraries the shipped binary links, through pkg-config, and nothing else:
libwayland-client, libwayland-egl, libEGL, libGLESv2, libfontconfig and
libfreetype. A test holds the binary to exactly this set.

**Owner goroutine**:
The one goroutine, pinned with `runtime.LockOSThread`, that owns a C object (the
`wl_display` and its proxies, the EGL/GL context, the fontconfig config, an
`FT_Library` or `FT_Face`). It releases what it owns explicitly; finalizers never
do.

**Protocol glue**:
The C code `wayland-scanner` writes from a Wayland protocol's XML file on
`go generate` (a client header and the interface tables), committed in
`internal/wlproto`. The **Go-side wrappers** over it are generated from the same
XML by logis's own generator.

**EGL Wayland platform**:
How the renderer presents: an EGL window surface on a `wl_egl_window` for the
layer surface, shown with `eglSwapBuffers`.

**Shaper**:
logis's own OpenType shaping engine (GSUB, GPOS, normalisation, bidi, Arabic and
Hebrew joining, grapheme clusters). It decides which glyphs go where; FreeType only
draws them. `internal/ot` applies the lookups, `internal/shape` shapes one run as
HarfBuzz does, and `text.Fonts.Layout` sets a line as cosmic-text does for the Rust
reference (bidi runs, words, fallback, tabs, columns, the ellipsis).

**Hinting**:
Fitting glyph outlines to the pixel grid, done by FreeType (TrueType bytecode, the
CFF hinter, the autohinter for unhinted fonts), the engine skrifa emulates for the
Rust reference.

**Client-side XKB**:
logis's own pure-Go keyboard: it parses the compositor's keymap, tracks state from
its modifier masks, and resolves keysyms, consumed modifiers, Compose and repeat.

**Test-only oracle**:
A library linked only into tests to check logis's own code against (libxkbcommon
for the keyboard, `fc-match` for font resolution); never part of the shipped
binary.

**Differential suite**:
The tests that run the same inputs through logis's own code and a test-only oracle
and require the same answers.

**Recorded oracle**:
A reference library's answers over a fixed corpus, committed as test data next to
the standalone program that produced them, where the library cannot be linked into
Go tests: `internal/fuzzy/testdata/nucleo.json`, from nucleo-matcher 0.3.1 as the
Rust reference configured it; `internal/shape/testdata/hb.json`, from the system
libharfbuzz through Python ctypes (runs over the pinned fonts, subsets of system
fonts and a synthetic layout font); `internal/text/testdata/cosmic.json`, from
cosmic-text 0.19 laying out lines as Rust logis v1 does. The differential suites
compare with them.

**Divergence**:
A case where the Go build deliberately answers differently from the Rust reference,
pinned with its reason; for the matcher, the nucleo defects it fixes
(`internal/fuzzy/testdata/divergences.json`; visible in the list's order when a
row with a non-ASCII character ties with others, as in the theme window golden);
for line layout, cosmic-text dropping the leading run of a right-to-left line that
starts with digits, and cosmic-text setting the whole word in the match face when a
match starts an ASCII word, where logis sets only the matched letters apart
(`divergences` in `internal/text/layout_test.go`).

## Grimdark UI vocabulary

The words flavoured text draws on. Each stands for the plain term in brackets, and a
message using one still states the plain fact (for example the missing protocol's
name).

**Cogitator** [the logis window]: what the user sees and types into.

**Machine spirit** [the daemon]: the resident process behind every cogitator.

**Noosphere** [the Wayland session]: the connection to the compositor; "the
noosphere is silent" means no compositor answers.

**Altar** [a compositor offering layer shell]: where a cogitator can be raised.

**Rune** [a command-line flag]: as in servitor; an unknown rune is an error (exit 2).

**Vox-cast** [toast]: a themed notice, in servitor's sense of sending word.

**Heresy** [an error]: what invalid configuration or a failed invocation reports.
