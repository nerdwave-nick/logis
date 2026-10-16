# The provider protocol

**Version 1.** Everything logis shows comes from a *provider* (spec §4.8): the
built-in dmenu provider (a client's stdin), servitor mode (built in, and spoken
over this protocol: [below](#servitor-mode-on-this-protocol)), the app launcher
(built in), and external providers: any program that speaks this protocol. An
external provider fills one window with one or more *stages* (lists),
is told what the user chose, and can show *toasts*. It is a process, never a
plugin (no ABI).

The code is [`internal/provider/`](../internal/provider/): `wire.go` (these
messages), `stages.go` (item ids to list rows), `process.go` (spawning, reading,
signals), `external.go` (greeting and retirement rules). Built-in providers produce the same
messages: the dmenu provider in-process, servitor mode as a process of logis
itself ([below](#servitor-mode-on-this-protocol)).

## Running a provider

```sh
logis --provider PROGRAM [ARG]...
```

Everything after `--provider` is the provider's command line. The daemon starts it
**as if this client had**: in the client's working directory, with the client's
environment (never the daemon's own, which may be stale), in a **process group of
its own**. The provider's stdin and stdout are pipes to the daemon; its stderr goes
to the daemon's log (`$XDG_RUNTIME_DIR/logis.log` for a daemon a client started).
The session is interactive: it replaces a visible session, and a new app, servitor
or provider session replaces it.

| Exit | When |
|---|---|
| 0 | the provider ended its flow (`close`) |
| 1 | the user dismissed the window, or another session replaced it |
| 2 | an error, on stderr: the provider could not start, broke the protocol, showed no stage within 5 s, or exited before `close` (as for every logis error) |

## Lines

Each message is one line of UTF-8 JSON (an object, ended by `\n`), at most
16 MiB, tagged by its `"type"`. Blank lines are skipped. **Unknown fields are
ignored** and **lines of an unknown type are skipped** (and logged), so a later
version can add optional fields and message types without breaking older peers.
A line that is not JSON, or a known type with a malformed field, is a protocol
error: the session ends with exit 2 and the provider is cancelled.

### From logis to the provider

| `type` | Fields | Meaning |
|---|---|---|
| `hello` | `protocol` (1) | logis's first line: the version it speaks |
| `activate` | `stage`, `id` | the user chose item `id` of stage `stage` (Return) |
| `submit` | `stage`, `text` | the user submitted typed text in stage `stage` |
| `cancel` | — | the window is gone without your `close`: dismissed (Escape, focus loss), replaced, or its caller left. Finish what you started and exit. |

### From the provider to logis

| `type` | Fields | Meaning |
|---|---|---|
| `hello` | `protocol` | the provider's **first** line: the version it speaks (must be 1) |
| `stage` | see [Stage](#stage) | a new list; the first one opens the window, a later one replaces the list in the same window |
| `items` | `items`: [Item] | items for the current stage: a new `id` is added after the others, a known `id` is replaced in its place (an *upsert*); optional `select` |
| `remove` | `ids`: [string] | items of the current stage that are gone (unknown ids are ignored) |
| `toast` | `text`, `level` (`info` default, `success`, `error`), `seconds` (default 3, 1 to 60) | a short notice over the bottom of the list, for that long |
| `ready` | — | after an `activate`/`submit`: the current stage takes Return again |
| `close` | — | the flow is over: the caller exits 0 and the window closes |

`items` and `remove` before the first `stage` are protocol errors; a `toast`
before it is only logged.

### Stage

| Field | Default | Meaning |
|---|---|---|
| `id` | `""` | the provider's name for the stage, echoed in `activate`/`submit` |
| `prompt` | `"> "` | shown before the query |
| `placeholder` | `""` | shown while the query is empty |
| `query` | `""` | the query the stage opens with (a decreed default, say) |
| `input` | `"none"` | what Return does when no item matches: `none` (nothing; only items can be chosen), `optional` (submit the typed text, even empty), `required` (submit it once it is not empty) |
| `select` | — | the id of the item to select: now, or once it arrives (in a later `items`) and is settled; never once the user has moved the selection or typed |
| `lines` | the theme's (9) | rows the window has room for (first stage only) |
| `width` | the theme's (820 px) | the window's width in characters (first stage only) |
| `items` | `[]` | the stage's first items |

The first stage sizes the window; later stages keep that size.

### Item

| Field | Default | Meaning |
|---|---|---|
| `id` | `""` | stable within its stage: `items`, `remove`, `select` and `activate` name it |
| `columns` | `[]` | shown left to right, lined up with the other rows' columns (each as wide as its widest text shown so far, the last taking the rest; a tab or line break inside a column becomes a space) |
| `match` | the columns | the text the query is matched against instead (then nothing is highlighted) |
| `icon` | — | a freedesktop icon name or an absolute path, for the icon column |
| `glyph` | — | a short text for the icon column (an emblem such as `⚙`) |
| `badges` | `[]` | short labels at the row's end, such as `current` |
| `pending` | `false` | the item's data has not settled: it is drawn dimmed with a "divining…" mark, **never activated** (Return on it does nothing) **and never preselected**; an upsert settles it |

The icon column appears when any item of the stage has a glyph or icon. An icon
is looked up in the configured icon theme (then hicolor and the pixmaps) and
drawn in place of the glyph; an item whose icon is not found shows its glyph.

Shift+Return sends `activate` just as Return does (only the built-in app
launcher gives it a meaning of its own: an app's desktop actions).

## Rules

- **Greeting.** logis writes `hello` first; the provider's first line must be
  `hello` with the same protocol version, or the session ends with an error.
- **Time.** The first `stage` is due within **5 s** of the start; otherwise the
  session ends with exit 2. Show a stage at once and fill it later (with pending
  rows or a placeholder such as "the census is taken…") when your data is slow.
- **One answer at a time.** After an `activate` or `submit`, Return does nothing
  until the provider sends a new `stage` or `ready` (so a held or repeated Return
  cannot activate twice). Escape always dismisses.
- **Ending.** `close` ends the session: the caller is answered at once (exit 0).
  If a toast is showing, the window stays on screen until its time is over, but
  gives up the keyboard at once; a new session replaces it immediately. Without
  your `close` (dismissal, replacement, the caller leaving), logis writes
  `cancel` and closes your stdin.
- **Signals and actions.** logis signals only the provider process itself
  (through a pidfd), never its process group or the children it started, and:
  - a provider that was **never sent** an `activate` or `submit` cannot have
    started an action. After `cancel` and end of input it gets 1 s to exit, then
    SIGTERM, then after another 1 s SIGKILL;
  - a provider that **was sent** one may be carrying out an action, which
    dismissal must never kill: logis never signals it, it only closes its stdin
    and lets it finish.
- **Starting actions.** An action that should outlive the provider (launching a
  program, `servitor invoke …`) should run in its own session (`setsid`) with
  its stdio redirected away from the provider's pipes. logis keeps reading the
  provider's stdout until every holder has closed it (it never closes it under a
  child that still writes), and notices the provider's own exit even while a
  child still holds the pipe.
- **Exiting early.** A provider that exits before `close` while its session is
  open ends the session with an error (exit 2), naming its exit status.

## Versions

The `hello` lines carry the protocol version. New optional fields and new message
types keep the version (old peers ignore or skip them); a change in the meaning of
an existing message bumps it, and logis then refuses providers of another version
with a clear error. (The client ↔ daemon socket protocol has its own version,
now 5; a daemon refuses a client of any other version, older or newer, asking
for the daemon to be restarted, and that client exits 2; see
[`internal/protocol`](../internal/protocol/protocol.go).)

## Examples

A two-stage flow, as the test fixture
[`tests/fixtures/providers/forge.sh`](../tests/fixtures/providers/forge.sh) runs
it (`→` from logis, `←` from the provider; lines wrapped here for reading):

```text
→ {"type":"hello","protocol":1}
← {"type":"hello","protocol":1}
← {"type":"stage","id":"rite","prompt":"rite: ","placeholder":"choose a rite",
   "width":44,"lines":6,"items":[
   {"id":"oil","columns":["rite of oil","unguent"],"glyph":"⚙","badges":["current"],"pending":true},
   {"id":"fire","columns":["rite of ignition","spark"],"glyph":"†"}]}
                       (Return on the pending row does nothing)
← {"type":"items","items":[{"id":"oil","columns":["rite of oil","unguent"],"glyph":"⚙","badges":["current"]}]}
→ {"type":"activate","stage":"rite","id":"oil"}
← {"type":"stage","id":"aspect","prompt":"aspect: ","items":[
   {"id":"on","columns":["anointed"],"badges":["current"]},{"id":"off","columns":["dry"]}]}
→ {"type":"activate","stage":"aspect","id":"on"}
← {"type":"toast","text":"the rite of oil is begun","level":"success","seconds":8}
← {"type":"close"}
```

A prompt for free text (a mandatory value with no default):

```json
{"type":"stage","id":"inscription:target","prompt":"target: ","placeholder":"the vessel to anoint","input":"required"}
```

and logis answers `{"type":"submit","stage":"inscription:target","text":"…"}`.

The smallest provider, in sh: one stage, and whatever is chosen is printed to the
daemon's log.

```sh
#!/bin/sh
read -r hello
echo '{"type":"hello","protocol":1}'
echo '{"type":"stage","id":"pick","items":[{"id":"a","columns":["first"]},{"id":"b","columns":["second"]}]}'
while read -r line; do
    case $line in
    *'"activate"'*) echo "chosen: $line" >&2; echo '{"type":"close"}'; exit 0 ;;
    *'"cancel"'*) exit 0 ;;
    esac
done
```

## servitor mode on this protocol

servitor mode (`logis --servitor`, spec §4.8; the code is
[`internal/servitor/`](../internal/servitor/)) is a provider session whose provider is
logis itself: the client asks the daemon to run `logis provide servitor` (its
own executable), which the daemon starts like any provider, as the caller
would have: the caller's working directory and environment (so `servitor` is
found on the caller's `PATH` and its notifications reach the caller's desktop),
a process group of its own. Everything in it is protocol version 1, and the
rules above protect the invocation: once it was sent a choice, logis never
signals it, nor anything it started.

| servitor | provider message |
|---|---|
| `servitor census --vigil`, started once the first stage is out | a `rite` stage at once (placeholder "choose a rite"), one item per rite as its line arrives, `id` = rite name; columns: name, aspect (`·` while unknown), purpose |
| a later `rite` line of the same rite (it replaces the earlier one) | `items` with that `id` (upsert in place) |
| `auspex` `"awaited"` | the item with `pending: true`: drawn as pending, never chosen, never preselected (story 10); the provider also answers `ready` to an `activate` of it |
| the auspex settles (`heeded`, `mute`) | `items` with `pending: false` and the aspect it settled |
| an edict, a rite whose standing is heretical, corrupted or tainted, or desecrated | `badges`: `edict`, `heresy`, `corrupted`, `tainted`, `desecrated`; glyph `⚙` for a rite, `†` for an edict |
| `lament` lines, unknown heralds, unknown keys | passed over (laments go to the daemon's log) |
| the aspect stage | `stage` `aspect`, the aspects in written order; the current one has the `current` badge and is the stage's `select` only when the entry is settled and its aspect is one of its own (`auspex` `""`, `heeded` or `mute`; never `awaited`, `forgone` or a marker yet unknown) |
| a later line of the chosen rite while its aspects show | `items` with the aspects again, marked and selected by the same rule |
| an inscription of the `canon` (`key`, `purpose`, `mandatory`, `decrees`, `decree`), in written order | a stage per inscription, `id` `inscription:<key>`: `prompt` `<key>: ` (`<key>*: ` when mandatory), `placeholder` = purpose (else "the inscription <key>"), `query` = `decrees[aspect]` (or the edict's `decree`), `input` = `required` when mandatory, else `optional` |
| `servitor invoke <rite> [<aspect>] [--<key> <word>]...` | after the last answer: every word given, and `--<key> ""` where an empty word clears a decree; an empty word without a decree is left out. Started detached (servitor S7: `setsid`, stdin, stdout and stderr on `/dev/null`, an exec error reported at once through a pipe closed on exec), never waited for; then `close` **without a toast**: servitor announces the verdict itself (owner decision ae44412a) |

What logis itself cannot do is told as an error `toast` (6 s), never as an
invocation's outcome: no `servitor` to start (the rite stage stays empty), a
census that ends before `concluded`, an empty Librarium (an `info` toast), a
heretical rite chosen (then `ready`), an empty mandatory word (then `ready`), an
invocation that could not be started (then `ready`, so Return may try again).
Dismissal ends the provider (`cancel`, end of input), which closes the census's
stdout; servitor then ends the census and its auspexes itself.
