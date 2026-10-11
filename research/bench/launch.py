#!/usr/bin/env python3
"""Run a command N times with T0_NS=CLOCK_MONOTONIC just before exec and
collect the milestone lines it prints on stderr. Research tooling."""
import os, subprocess, sys, time, statistics, re, collections

n = int(sys.argv[1])
cmd = sys.argv[2:]
rows = collections.defaultdict(list)
for i in range(n):
    env = dict(os.environ, T0_NS=str(time.monotonic_ns()))
    p = subprocess.run(cmd, env=env, stderr=subprocess.PIPE, stdout=subprocess.DEVNULL, text=True)
    for line in p.stderr.splitlines():
        m = re.match(r"(.+?)\s+(-?[\d.]+) ms$", line.strip())
        if m:
            rows[m.group(1).strip()].append(float(m.group(2)))
    time.sleep(0.25)
for k, v in rows.items():
    v.sort()
    print(f"{k:26s} median {statistics.median(v):8.2f}  min {v[0]:8.2f}  max {v[-1]:8.2f}  (n={len(v)})")
