#!/usr/bin/env python3
"""Fake DG host for testing the emulator over a virtual serial pair.

Draws a D200 test screen using real Dasher control codes, then reports
every byte it receives from the terminal (keys, cursor-address reply,
BREAK) on the bottom rows, in octal, as a DG programmer would read them.

Usage: fakehost.py /dev/pts/N   (the other end of a socat pty pair)
"""
import os, select, sys, termios, time, tty

fd = os.open(sys.argv[1], os.O_RDWR | os.O_NOCTTY)
tty.setraw(fd)

def out(*parts):
    b = bytearray()
    for p in parts:
        b += p.encode("ascii") if isinstance(p, str) else bytes(p)
    os.write(fd, bytes(b))

def at(col, row):
    return bytes([0o20, col, row])

out([0o14])                                   # erase page
out(at(22, 0), [0o36, ord("D")], " DATA GENERAL DASHER D200 TEST ", [0o36, ord("E")])
out(at(2, 2), "Normal text   ", [0o34], "Dim text", [0o35], "   ",
    [0o24], "Underlined", [0o25], "   ", [0o16], "Blinking", [0o17])
out(at(2, 3), [0o36, ord("D")], " Reverse ", [0o36, ord("E")], "  ",
    [0o34, 0o36, ord("D")], " Dim reverse ", [0o36, ord("E"), 0o35], "  ",
    [0o24, 0o34], "Dim underlined", [0o25, 0o35])
# A box drawn purely with cursor addressing.
out(at(2, 5), "+" + "-" * 30 + "+")
for r in range(6, 10):
    out(at(2, r), "|", at(33, r), "|")
out(at(2, 10), "+" + "-" * 30 + "+")
out(at(5, 7), "Cursor-addressed box")
out(at(40, 6), "Erase-EOL test: XXXXXXXXXXXX", at(56, 6), [0o13])
out(at(40, 7), "Cursor keys: ", [0o30, 0o30], "<-gap")
out(at(40, 8), "Tab", [0o11], "stop", [0o11], "stop")
out(at(0, 12), "R.", [0o12])                     # RDOS-style prompt on NL
# Ask the terminal where its cursor is (005); the reply is shown below.
out(at(17, 13), [0o05])
out(at(0, 13), "Cursor report: ")
out(at(0, 15), "Type keys - each byte received is shown below in octal:")
out(at(0, 16))

log = []
while True:
    r, _, _ = select.select([fd], [], [], 0.5)
    if not r:
        continue
    try:
        data = os.read(fd, 256)
    except OSError:
        break
    if not data:
        break
    log.extend(data)
    if data[:1] == b"\x1f":                       # cursor-address reply
        out(at(15, 13), "037 %03o %03o (col %d row %d)" % (data[1], data[2], data[1], data[2]))
        log = log[3:]
    text = " ".join("%03o" % b for b in log[-19:])
    out(at(0, 17), [0o13], "Host got: " + text)
