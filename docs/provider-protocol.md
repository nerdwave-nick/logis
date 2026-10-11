# The provider protocol

**Version 1.** Everything logis shows comes from a *provider* (spec §4.8): the
built-in dmenu provider (a client's stdin), the app launcher and servitor mode
(built in, to come), and external providers: any program that speaks this
protocol. An external provider fills one window with one or more *stages* (lists),
is told what the user chose, and can show *toasts*. It is a process, never a
plugin (no ABI).

The code is [`src/provider/`](../src/provider/): `wire.rs` (these messages),
`stages.rs` (item ids to list rows), `process.rs` (spawning, reading, signals),
`external.rs` (greeting and retirement rules). Built-in providers produce the same
messages in-process.

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
| `toast` | `text`, `level` (`info` default, `success`, `error`) | a short notice over the bottom of the list, for 3 s |
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
| `lines` | 15 | rows the window has room for (first stage only) |
| `width` | 30 | the window's width in characters (first stage only) |
| `items` | `[]` | the stage's first items |

The first stage sizes the window; later stages keep that size.

### Item

| Field | Default | Meaning |
|---|---|---|
| `id` | `""` | stable within its stage: `items`, `remove`, `select` and `activate` name it |
| `columns` | `[]` | shown left to right, lined up at tab stops (a tab or line break inside a column becomes a space) |
| `match` | the columns | the text the query is matched against instead (then nothing is highlighted) |
| `icon` | — | a freedesktop icon name or an absolute path, for the icon column |
| `glyph` | — | a short text for the icon column (an emblem such as `⚙`) |
| `badges` | `[]` | short labels at the row's end, such as `current` |
| `pending` | `false` | the item's data has not settled: it is drawn dimmed with a "divining…" mark, **never activated** (Return on it does nothing) **and never preselected**; an upsert settles it |

The icon column appears when any item of the stage has a glyph or icon. This
version of logis draws glyphs; named icons are carried to the window and drawn
once the list draws icons (the app launcher, ca5ef93c).

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
  If a toast is showing, the window stays on screen until its 3 s are over, but
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
3 since provider sessions; see [`src/protocol/mod.rs`](../src/protocol/mod.rs).)

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
← {"type":"toast","text":"the rite of oil is begun","level":"success"}
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

The servitor provider (9dcfd937 interim, 88d4f372 full) is built into the daemon
and speaks these messages in-process; nothing in it needs more than version 1:

| servitor | provider message |
|---|---|
| every rite at once (`census --vigil`, or `census --binharic` interim) | a `rite` stage at once (placeholder while the census runs), one item per rite, `id` = rite name |
| a census line that replaces an earlier one by rite name | `items` with that `id` (upsert in place) |
| an aspect still `awaited` (auspex unsettled) | the item with `pending: true`: drawn as pending, never chosen, never preselected (story 10) |
| the auspex settles | `items` with `pending: false` |
| edicts, heretical rites | `badges` (`edict`, `heresy`) |
| the aspect stage, the current aspect marked once known | `stage` `aspect`, the current one with a `current` badge and `select` once settled (never while pending or stale) |
| an inscription from `canon` (`key`, `purpose`, `mandatory`, `decrees`, `decree`) | a stage per inscription: `prompt` = key, `placeholder` = purpose, `query` = `decrees[aspect]` (or the edict's `decree`), `input` = `required` when mandatory without a decree, else `optional` |
| `servitor invoke <rite> [<aspect>] [--<inscription> v]...` | started detached (own session, stdio away) on the last `activate`/`submit`, then `close` **without a toast**: servitor announces the verdict itself (owner decision ae44412a) |

Dismissing the window at any point after the invocation started never aborts it:
logis never signals a provider it sent an answer to, nor any of its children.
