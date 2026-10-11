#!/usr/bin/env python3
"""Send N open requests (payload: CLOCK_MONOTONIC ns) to the spike daemon."""
import socket, sys, time, os
path = os.path.join(os.environ["XDG_RUNTIME_DIR"], "logis-spike.sock")
s = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
for i in range(int(sys.argv[1])):
    s.sendto(str(time.monotonic_ns()).encode(), path)
    time.sleep(1.4)
s.sendto(b"quit", path)
