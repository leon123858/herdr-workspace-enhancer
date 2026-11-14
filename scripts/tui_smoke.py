"""Exercise the actual TUI through a Linux PTY, with an isolated fake Herdr.

Usage: python3 scripts/tui_smoke.py [path/to/binary]
"""
import fcntl
import hashlib
import json
import sqlite3
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
import tomllib

binary = str(Path(sys.argv[1] if len(sys.argv) > 1 else 'bin/herdr-workspace-enhancer').resolve())
with tempfile.TemporaryDirectory() as tmp:
    root = Path(tmp)
    fake = root / 'herdr'
    fake.write_text('#!/usr/bin/env python3\nimport json\nprint(json.dumps({"result":{"workspaces":[{"workspace_id":"h1","label":"Example project"},{"workspace_id":"h2","label":"Second project"}]}}))\n')
    fake.chmod(0o755)
    clipboard = root / 'clipboard.txt'
    long_note = '中文筆記\n' * 149 + '最後一行'
    clipboard.write_text(long_note, encoding='utf-8')
    clip = root / 'xclip'
    clip.write_text('#!/usr/bin/env python3\nimport os,pathlib,sys\nsys.stdout.write(pathlib.Path(os.environ["ENHANCER_TEST_CLIPBOARD"]).read_text(encoding="utf-8"))\n')
    clip.chmod(0o755)
    socket = str(root / 'socket')
    store = root / 'sessions' / hashlib.sha256(socket.encode()).hexdigest()
    store.mkdir(parents=True)
    master, slave = pty.openpty()
    proc = None
    def size(width):
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 30, width, 0, 0))
        if proc is not None:
            proc.send_signal(signal.SIGWINCH)
    size(100)
    env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
               WAYLAND_DISPLAY='', ENHANCER_TEST_CLIPBOARD=str(clipboard), TERM='xterm-256color',
               HERDR_BIN_PATH=str(fake), HERDR_SOCKET_PATH=socket,
               HERDR_PLUGIN_STATE_DIR=tmp, HERDR_WORKSPACE_ID='manager')
    subprocess.run([binary, 'sync'], env=env, check=True, capture_output=True)
    database = store / 'state.sqlite'
    with sqlite3.connect(database) as db:
        ids = dict(db.execute('SELECT host_id, id FROM workspaces'))
        note_ids = {'one': ids['h1'], 'two': ids['h2']}
        db.execute('INSERT INTO workspaces (id, host_id, label, status, is_manager) VALUES (?, ?, ?, ?, ?)',
                   ('manager-record', 'manager', 'Workspace Enhancer', 'active', True))
        db.execute('UPDATE workspaces SET note = ? WHERE id = ?', ('Side preview', note_ids['two']))
    def read_note(identity):
        with sqlite3.connect(database) as db:
            text, = db.execute('SELECT note FROM workspaces WHERE id = ?',
                               (note_ids[identity],)).fetchone()
            return text
    output = bytearray()
    proc = None
    def read(seconds=.25):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if select.select([master], [], [], .05)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
    def send(data):
        os.write(master, data)
        read()
    def click(x, y):
        send(f'\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m'.encode())
    def start():
        return subprocess.Popen([binary, 'tui'], stdin=slave, stdout=slave, stderr=slave, env=env)
    try:
        proc = start()
        read(1.5)
        assert b'Example project' in output, output
        assert b'\x1b[?1002h' in output, 'mouse reporting disabled'
        click(3, 7)
        assert b'Side preview' in output, output
        send(b'\x1b[<0;36;5M')
        send(b'\x1b[<32;46;5M')
        send(b'\x1b[<0;46;5m')
        config_path = root / 'config.toml'
        ratio = tomllib.loads(config_path.read_text())['tui']['workspace_ratio']
        assert abs(ratio - .45) < .001, ratio
        size(60)
        read(.3)
        size(120)
        read(.3)
        click(58, 4)
        send(b'edited ')
        click(58, 26)  # Save button at the bottom of the note pane.
        assert 'edited' in read_note('two')
        copies = output.count(b'\x1b]52;c;')
        send(b'\x1b[<0;58;4M\x1b[<32;70;4M\x1b[<0;70;4m')
        assert output.count(b'\x1b]52;c;') == copies + 1, 'selection release did not copy'
        assert b'Selection copied' in output, 'copy notification missing'
        send(b'\x1b[<2;60;4M\x1b[<2;60;4m')
        assert output.count(b'\x1b]52;c;') == copies + 2, 'right click did not copy selection'
        send(b'\x1b')
        click(3, 6)
        send(b'e\x16')  # Paste a 150-line Unicode note.
        read(.4)
        send(b'\x13')
        assert read_note('one') == long_note
        send(b'\x1b')
        send(b'q')
        read(.3)
        proc.wait(timeout=5)
        assert proc.returncode == 0, output
        # Reopen at another terminal width; loading must preserve the preference.
        proc = start()
        read(1)
        send(b'q')
        proc.wait(timeout=5)
        assert proc.returncode == 0, output
        assert tomllib.loads(config_path.read_text())['tui']['workspace_ratio'] == ratio
        print('PASS: mouse preview/edit/save; automatic/right-click copy and notification; divider persistence; resize; Unicode paste; restart')
    finally:
        if proc is not None and proc.poll() is None:
            proc.terminate()
            proc.wait(timeout=5)
        os.close(master)
        os.close(slave)
