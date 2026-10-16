# Research code (disposable)

Benchmark code for the one-shot cold-start measurements cited by
[ADR 0001](../docs/adr/0001-resident-daemon-and-thin-client.md) (launch → first committed
frame for wl_shm, raw Vulkan, EGL + GLES2, GTK4 and Qt Quick). This is **not** a foundation for
logis and is not part of the build; it will be thrown away.

Method: `bench/launch.py` takes CLOCK_MONOTONIC just before `exec` and passes it as `T0_NS`; the
program prints its milestones in ms relative to it, so exec and dynamic linking are included. The
latency harness (`internal/e2e/latency`) uses the same `T0_NS` method. Toolkits that print nothing were timed from
`WAYLAND_DEBUG` timestamps instead (`bench/wldebug_timing.py`, exec → first buffer
attach+commit); that clock is wall time, so the two methods are not perfectly comparable.
"Committed" is not presented.

| Path | What it measures | Run |
|---|---|---|
| `bench/firstframe.c` | launch → first commit/presentation for wl_shm, EGL/GLES2 and Vulkan on a layer surface (`ff-shm`, `ff-egl`, `ff-vk`) | build below; `python3 bench/launch.py 10 ./bench/ff-shm shm` |
| `bench/launch.py` | runs a program N times with `T0_NS` = CLOCK_MONOTONIC before exec, aggregates its milestones | — |
| `bench/wldebug_timing.py` | exec → first attach+commit from `WAYLAND_DEBUG` (UTC) timestamps, for toolkits | `python3 bench/wldebug_timing.py 6 1.5 qs -p bench/qml/shell.qml` |
| `bench/qml/shell.qml`, `bench/gtk4hello.c` | minimal Qt Quick (Quickshell) and GTK4 windows | — |

Build, from `bench/` (the Vulkan variant needs the Vulkan headers):

```sh
P="firstframe.c wlr-layer-shell-unstable-v1-protocol.c xdg-shell-protocol.c presentation-time-protocol.c"
gcc -O2 -o ff-shm $P $(pkg-config --cflags --libs wayland-client)
gcc -O2 -DWITH_EGL -o ff-egl $P $(pkg-config --cflags --libs wayland-client wayland-egl egl glesv2)
gcc -O2 -DWITH_VK -o ff-vk $P $(pkg-config --cflags --libs wayland-client vulkan)
gcc -O2 -o gtk4hello gtk4hello.c $(pkg-config --cflags --libs gtk4)
```

Every window here except the dmenu-style fuzzel comparison sets keyboard interactivity to none, so these programs never take keyboard focus.
