import os,pty,subprocess,tempfile,pathlib,hashlib,json,select,time,fcntl,termios,struct
with tempfile.TemporaryDirectory() as tmp:
 root=pathlib.Path(tmp);fake=root/'herdr';fake.write_text('#!/usr/bin/env python3\nimport json\nprint(json.dumps({"result":{"workspaces":[{"workspace_id":"w-project","label":"Example project"}]}}))\n');fake.chmod(0o755)
 clipboard=root/'clipboard.txt';clipboard.write_text('Example',encoding='utf-8')
 clip=root/'xclip';clip.write_text('#!/usr/bin/env python3\nimport os,pathlib,sys\nsys.stdout.write(pathlib.Path(os.environ["ENHANCER_TEST_CLIPBOARD"]).read_text(encoding="utf-8"))\n');clip.chmod(0o755)
 socket=str(root/'socket');store=root/'sessions'/hashlib.sha256(socket.encode()).hexdigest();store.mkdir(parents=True)
 (store/(hashlib.sha256(b'manager-workspace-id').hexdigest()+'.json')).write_text(json.dumps({'revision':1,'text':'manager'}))
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',30,100,0,0))
 env=dict(os.environ,PATH=str(root)+os.pathsep+os.environ['PATH'],WAYLAND_DISPLAY='',ENHANCER_TEST_CLIPBOARD=str(clipboard),TERM='xterm-256color',HERDR_BIN_PATH=str(fake),HERDR_SOCKET_PATH=socket,HERDR_PLUGIN_STATE_DIR=tmp,HERDR_WORKSPACE_ID='manager')
 proc=subprocess.Popen(['bin/herdr-workspace-enhancer'],stdin=slave,stdout=slave,stderr=slave,env=env);os.close(slave)
 output=bytearray()
 def read(seconds):
  deadline=time.time()+seconds
  while time.time()<deadline:
   if select.select([master],[],[],0.1)[0]:
    try:output.extend(os.read(master,65536))
    except OSError:break
 try:
  read(2)
  assert b'Example project' in output,repr(output)
  os.write(master,b'/');read(.2);os.write(master,b'\x16');read(.5)
  assert b'Example' in output
  os.write(master,b'\r');read(.2)
  long_note='中文筆記\n'*149+'最後一行';clipboard.write_text(long_note,encoding='utf-8')
  os.write(master,b'e');read(.3);os.write(master,b'\x16');read(.8);os.write(master,b'\x13');read(.4);os.write(master,b'\x1b');read(.4);os.write(master,b'q');proc.wait(timeout=5)
  notes=[json.loads(p.read_text()) for p in store.glob('*.json') if p.name!='workspaces.json']
  assert any(n.get('text')==long_note for n in notes),notes
  print('PASS: rendered workspace list; pasted and saved 150-line Chinese note; exited normally')
 finally:
  if proc.poll() is None:proc.terminate();proc.wait(timeout=5)
  os.close(master)
