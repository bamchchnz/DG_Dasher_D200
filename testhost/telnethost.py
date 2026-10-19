#!/usr/bin/env python3
"""Minimal telnet server for testing the emulator's telnet client.

Negotiates ECHO/SGA/TTYPE, asks for the terminal type, echoes typed
characters, reports a received BREAK and checks IAC (0xFF) escaping.
Usage: telnethost.py PORT
"""
import socket, sys

IAC, DONT, DO, WONT, WILL, SB, SE, BRK = 255, 254, 253, 252, 251, 250, 240, 243
ECHO, SGA, TTYPE = 1, 3, 24

srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", int(sys.argv[1])))
srv.listen(1)
c, _ = srv.accept()
c.sendall(bytes([IAC, WILL, ECHO, IAC, WILL, SGA, IAC, DO, TTYPE]))
c.sendall(bytes([IAC, SB, TTYPE, 1, IAC, SE]))
c.sendall(b"\x0cDG TELNET TEST HOST\r\n")

state, sb, ttype = 0, b"", None
while True:
    data = c.recv(1024)
    if not data:
        break
    out = bytearray()
    for b in data:
        if state == 0:
            if b == IAC:
                state = 1
            else:
                out.append(b)
        elif state == 1:
            if b == IAC:
                out.append(IAC)          # escaped data byte 0xFF
                c.sendall(b"\r\n[got data byte 377]\r\n")
                state = 0
            elif b in (DO, DONT, WILL, WONT):
                state = 2
            elif b == SB:
                sb, state = b"", 3
            elif b == BRK:
                c.sendall(b"\r\n[BREAK received]\r\n")
                state = 0
            else:
                state = 0
        elif state == 2:
            state = 0
        elif state == 3:
            if b == IAC:
                state = 4
            else:
                sb += bytes([b])
        elif state == 4:
            if b == SE:
                if sb[:2] == bytes([TTYPE, 0]):
                    ttype = sb[2:].decode()
                    c.sendall(("Terminal type reported: %s\r\n" % ttype).encode())
                state = 0
            else:
                sb += bytes([b]); state = 3
    if out:
        c.sendall(bytes(out).replace(b"\n", b"\r\n").replace(b"\xff", b""))
