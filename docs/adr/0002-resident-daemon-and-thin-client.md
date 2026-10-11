# A resident daemon and a thin client in one binary

Status: accepted (2026-10-11, owner decisions on ddba629b, /clarify Q2 and the Q7
addendum, and the spec follow-up on session policy and systemd units).
Evidence: [`docs/research/stack.md`](../research/stack.md) (verdict, §2.1–§2.2
measurements, §5.1 process model, §7 risks).

A GPU path costs 12–70 ms at cold start (device creation, shaders, fonts), and every
one-shot launcher measured, fuzzel included, took 22–38 ms to its first committed
frame (stack.md §2.1). Opening from a keybinding must feel instant, and the look
needs the GPU (ADR 0001), so the expensive state has to stay warm. We decided on a
**resident daemon plus a thin client, both in the single `logis` binary**:

- The **daemon** owns the Wayland connection, the GPU device (created at start, not
  lazily), font and glyph caches, the app index and the providers. It shows and
  hides layer surfaces per session.
- Every user-facing invocation (`--dmenu`, app mode, servitor mode) is a **client**.
  It connects to `$XDG_RUNTIME_DIR/logis.sock`; if none answers, it **starts the
  daemon itself** and waits. An optional systemd user socket unit may start it
  instead; `logis.socket` and `logis.service` ship in the repository and the user
  enables them.
- The daemon **stays resident** by default. An optional `idle-timeout` setting (off
  by default) lets it exit when idle; the next open restarts it.
- Client and daemon speak a **framed Unix stream** with explicit sessions,
  completion and cancellation. A client's disconnect cancels its session. Each
  item's original input bytes are preserved separately from its display text. The
  result goes only to the client that opened the session. (The message format is a
  ticket-level design.)
- **Concurrent sessions:** while a session is visible, a new dmenu session queues
  (each caller gets its answer in order) and a new interactive session (app
  launcher, servitor) replaces the visible one.
- On compositor disconnect the daemon exits non-zero; the next client start (or the
  socket unit) restarts it.

The spike's daemon mode measured **6.5–8.9 ms** from client request to committed
frame (6.0–6.9 ms after fixing its drop order), against 22–38 ms for one-shot
launchers; its first open with a lazily created GPU device took 36–45 ms, and it held
**27 MB RSS** (stack.md §2.2). Resident designs are also how the fast-feeling
launchers in the landscape work: walker, anyrun, vicinae, hyprlauncher, Quickshell
shells (stack.md §3).

## Considered Options

- **One-shot process per invocation** — 22–28 ms with CPU rendering, 52–74 ms for
  the GPU spike (stack.md §2.1); too slow for the GPU look.
- **A one-shot render as fallback when no daemon runs** (stack.md §5.1) — not
  adopted: the client starts the daemon itself and waits, so there is one renderer
  and one code path (first open after login ~50–75 ms).
- **Socket activation only** — not adopted as the only start path: the units are
  shipped but the user enables them, so the client must be able to start the daemon
  without them.
- **Exit after an idle timeout by default** — not adopted: resident by default, the
  timeout is opt-in (Q7 addendum).
- **Fire-and-forget datagrams** (the spike's protocol) — only measured opening; real
  sessions need completion, cancellation and per-client results (stack.md §5.1).
- **A QML front end inside the owner's running Quickshell** — avoids a cold start
  but couples logis to the shell's lifecycle and is not a standalone program
  (stack.md §4.2).

## Consequences

- logis has two roles to keep apart in one binary, and a client ↔ daemon protocol
  to design and test (framing, sessions, cancellation, queue-versus-replace).
- Daemon lifecycle work is required (stack.md §5.1, §7): exit and restart on
  compositor disconnect, live reload of config and themes, inotify on desktop entry
  directories, and launching apps into the session environment (systemd user
  environment) with xdg-activation tokens, never the daemon's stale environment.
- Dismissing a picker never kills an action already running (such as a servitor
  invocation), because actions belong to providers, not to the window.
- The performance budgets (spec §4.10) assume this model: warm open ≤ 15 ms, first
  open after the client starts the daemon ≤ 150 ms, idle daemon RSS ≤ 64 MB and no
  wake-ups beyond event waits.
