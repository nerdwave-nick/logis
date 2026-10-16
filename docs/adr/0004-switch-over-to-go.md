# The switch-over: Go replaces Rust on `main`

Status: accepted (2026-10-16, owner decisions on the switch-over ticket e0f2c468:
the approved design implemented on `go` first, then the switch-over, delegated to
the coordinator; `opengl-driver` instead of `mesa`; the latency budgets not a
gate). Carries out ADR 0003's D6. Shared specification: lit project
`feat/logis-go`, §8 D6.

The Go build on the branch `go` reached what D6 asked: every E2E scenario carried
over from `feat/logis` passes in a nested niri, and the owner installed the Go
package on his machine and found it good. We decided:

- **One merge of `go` into `main` removes the Rust product**: the crate (`src/`,
  `Cargo.toml`, `Cargo.lock`, `clippy.toml`), its tests (`tests/*.rs`,
  `tests/support/`), its CI (`.github/workflows/ci.yml`) and the Rust research
  prototype (`research/proto-rust`). Rust stays in the history: `main` up to
  2ffdd02 is Rust logis v1. There is no Rust tag and no release.
- **What v1 left as data stays as data.** `cmd/logis/testdata/parity.json` is the
  frozen v1 record the parity test holds every rune to. Of v1's goldens under
  `tests/goldens/<set>/`, the ones a Go test still reads stay (the text corpus and
  faces, the icons, forge's window, the debug panel's captures, the harness
  reference); the 40 that a Go golden under `tests/goldens/go/` shadows and no
  test reads are removed, and nothing is re-recorded. The list of v1's 55 E2E
  tests and their Go ports is snapshotted in `cmd/logis/e2emap_test.go`; the
  README's caller contract names its Go tests instead of v1's.
- **The two recorded oracles stay Rust**: `internal/fuzzy/testdata/oracle`
  (nucleo-matcher) and `internal/text/testdata/cosmic-oracle` (cosmic-text). They
  are standalone programs in their own Cargo workspaces, still build offline and
  reproduce their JSON byte for byte; the Go tests say how to regenerate it. Only
  that needs cargo; building and testing logis do not.
- **The version's single source is `internal/version`**; the packaging tests hold
  the PKGBUILD, `.SRCINFO`, man page and changelog to it.
- **Packaging**: the PKGBUILD depends on `opengl-driver` (any vendor's EGL and
  OpenGL ES driver behind libglvnd: Mesa, NVIDIA's) instead of `mesa`, and has no
  `optdepends`. Its source stays the checkout's `HEAD` until a version is tagged.
- **CI** is `go.yml` alone, on every push and pull request that touches what the
  build and the tests read, in the pinned Arch Linux image.
- **Comments that name a Rust file** (`Rust src/…`, `tests/….rs::…`) stay: they
  say which part of v1 a Go file or test ports, and refer to 2ffdd02 in the
  history.

## Considered Options

- **Keep the Rust tree beside the Go build** (in a subdirectory, or on `main`
  untouched) — rejected: D6 asked for one merge that replaces it, and a second
  build that nothing tests rots.
- **A `rust-v1` tag** — rejected by the specification (no Rust tag); the commit
  2ffdd02 names the last Rust state well enough.
- **Re-record every golden under Go and drop v1's** — rejected: the v1 goldens
  that remain are the reference the Go renders are bounded against; re-recording
  them would turn the comparison into a self-comparison.
- **Retire the Rust oracles and keep only their data** — rejected while they still
  build: regenerating the data from the real libraries is what makes it an
  oracle.

## Consequences

- logis builds and tests with Go and a C toolchain alone; cargo is needed only to
  regenerate the two recorded oracles.
- ADR 0001 describes v1's Rust and wgpu foundations, which are gone; its "no CPU
  renderer" rule holds on through ADR 0003. ADR 0002 holds unchanged.
- A v1 daemon left running across the upgrade refuses the Go clients (socket
  protocol 4 against v1's 3) with its reason; users restart it once.
- Divergences from v1 that users see are listed in the changelog's entry for the
  switch.
