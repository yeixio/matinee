#!/usr/bin/env python3
"""Plays a MAME set with matinee-game-runner for a few seconds and checks
it draws a picture: the arcade emulator's smoke test.

    play.py <runner> <core.so> <set.zip> [seconds]
"""
import os
import struct
import subprocess
import sys
import tempfile
import time

runner, core, game = sys.argv[1:4]
seconds = float(sys.argv[4]) if len(sys.argv) > 4 else 20
save, system = tempfile.mkdtemp(), tempfile.mkdtemp()
p = subprocess.Popen([runner, core, game, save, system], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
info, frames, lit = None, 0, 0
deadline = time.time() + seconds
try:
    while time.time() < deadline:
        head = p.stdout.read(8)
        if len(head) < 8:
            break
        kind, size = chr(head[0]), struct.unpack("<I", head[4:])[0]
        body = p.stdout.read(size)
        if kind == "E":
            sys.exit("runner: " + body.decode(errors="replace"))
        if kind == "I":
            info = body.decode()
            print("info", info)
        elif kind == "V":
            frames += 1
            # A frame with anything other than black in it.
            if body.strip(b"\x00"):
                lit += 1
finally:
    p.stdin.close()
    p.kill()
print(f"{frames} frames, {lit} with a picture")
if info is None or frames < 60 or lit == 0:
    sys.exit("the game didn't play")
