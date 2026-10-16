# Research code (disposable)

Evidence for `docs/research/stack.md`. This is **not** a foundation for logis; it will be thrown away. The Rust prototypes it measured (`research/proto-rust`: the CPU path, the wgpu cold start and the GPU/daemon spike) were removed at the switch-over to Go (ADR 0004) and are in the history up to `main` 2ffdd02.

| Path | What it measures | Run |
|---|---|---|
| `bench/firstframe.c` | launch → first commit/presentation for wl_shm, EGL/GLES2 and Vulkan on a layer surface | `gcc` lines in stack.md §2; `python3 bench/launch.py 10 ./bench/ff-shm shm` |
| `bench/launch.py` | runs a program N times with `T0_NS` = CLOCK_MONOTONIC before exec, aggregates its milestones | — |
| `bench/wldebug_timing.py` | exec → first attach+commit from `WAYLAND_DEBUG` (UTC) timestamps, for toolkits | `python3 bench/wldebug_timing.py 6 1.5 qs -p bench/qml/shell.qml` |
| `bench/qml/shell.qml`, `bench/gtk4hello.c` | minimal Qt Quick (Quickshell) and GTK4 windows | — |
| `bench/daemon_client.py` | sends open requests to the spike daemon (the removed `proto-rust` spike, from the history) | `python3 bench/daemon_client.py 8` |
| `evidence/` | screenshots (`spike.png`, `spike-blur.png`) and run logs | — |

Every window here except the dmenu-style fuzzel comparison sets keyboard interactivity to none, so these programs never take keyboard focus.
