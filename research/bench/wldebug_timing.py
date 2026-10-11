#!/usr/bin/env python3
"""Launch a libwayland client with WAYLAND_DEBUG=1, kill it after a while, and
report the wall-clock delay from exec to its first buffer attach+commit."""
import os, re, signal, subprocess, sys, time, statistics, datetime
n, secs, cmd = int(sys.argv[1]), float(sys.argv[2]), sys.argv[3:]
res = []
for _ in range(n):
    t0 = time.time()
    p = subprocess.Popen(cmd, env=dict(os.environ, WAYLAND_DEBUG="client"), stderr=subprocess.PIPE, stdout=subprocess.DEVNULL, text=True, start_new_session=True)
    try:
        _, err = p.communicate(timeout=secs)
    except subprocess.TimeoutExpired:
        os.killpg(p.pid, signal.SIGKILL); _, err = p.communicate()
    err = re.sub(r"\x1b\[[0-9;]*m", "", err)
    # libwayland's WAYLAND_DEBUG clock is UTC wall time
    day = datetime.datetime.fromtimestamp(t0, datetime.timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0).timestamp()
    attached = False
    for line in err.splitlines():
        m = re.match(r"\[(\d+):(\d+):([\d.]+)\]", line)
        if not m: continue
        t = day + int(m.group(1)) * 3600 + int(m.group(2)) * 60 + float(m.group(3))
        if "-> wl_surface" in line and ".attach(" in line and "nil" not in line.split(".attach(")[1][:5]:
            attached = True
        if attached and "-> wl_surface" in line and ".commit()" in line:
            res.append((t - t0) * 1000); break
    time.sleep(0.3)
res.sort()
print(f"first attach+commit: median {statistics.median(res):.1f} ms  min {res[0]:.1f}  max {res[-1]:.1f}  (n={len(res)})" if res else "no commit seen")
