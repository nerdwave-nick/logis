# Research code (disposable)

Evidence for `docs/research/stack.md`. This is **not** a foundation for logis; it will be thrown away.

| Path | What it measures | Run |
|---|---|---|
| `bench/firstframe.c` | launch → first commit/presentation for wl_shm, EGL/GLES2 and Vulkan on a layer surface | `gcc` lines in stack.md §2; `python3 bench/launch.py 10 ./bench/ff-shm shm` |
| `bench/launch.py` | runs a program N times with `T0_NS` = CLOCK_MONOTONIC before exec, aggregates its milestones | — |
| `bench/wldebug_timing.py` | exec → first attach+commit from `WAYLAND_DEBUG` (UTC) timestamps, for toolkits | `python3 bench/wldebug_timing.py 6 1.5 qs -p bench/qml/shell.qml` |
| `bench/qml/shell.qml`, `bench/gtk4hello.c` | minimal Qt Quick (Quickshell) and GTK4 windows | — |
| `bench/daemon_client.py` | sends open requests to the spike daemon | `SPIKE=daemon proto-rust/target/release/spike & python3 bench/daemon_client.py 8` |
| `proto-rust/src/main.rs` | Rust CPU path: sctk + tiny-skia + cosmic-text (`FONTS=scan` for the full font scan) | `cargo build --release` |
| `proto-rust/src/bin/gpu.rs` | wgpu clear (`RENDERER=wgpu`) and wgpu + vello cold start | `cargo build --release --bin gpu` |
| `proto-rust/src/bin/spike.rs` + `panel.wgsl` | candidate GPU foundation: fractional scale, viewporter, wgpu, SDF shader panel, glyphon text, ext-background-effect blur, daemon mode | `SPIKE=anim`, `SPIKE=daemon`, `SPIKE_FULL=1`, `SPIKE_NOBLUR=1` |
| `evidence/` | screenshots (`spike.png`, `spike-blur.png`) and run logs | — |

Every window here except the dmenu-style fuzzel comparison sets keyboard interactivity to none, so these programs never take keyboard focus.
