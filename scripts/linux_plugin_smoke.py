"""Real Herdr startup check in an isolated Linux session; takes plugin root."""
import json
import sqlite3
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import uuid

plugin = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="enhancer-plugin-test-") as directory:
    env = dict(os.environ, XDG_CONFIG_HOME=directory + "/config",
               XDG_DATA_HOME=directory + "/data", XDG_STATE_HOME=directory + "/state",
               XDG_RUNTIME_DIR=directory, TERM="xterm-256color")
    env.pop("HERDR_SOCKET_PATH", None)
    session = "enhancer-smoke-" + uuid.uuid4().hex[:8]
    command = ["herdr", "--session", session]
    def call(*args):
        output = subprocess.check_output(command + list(args), env=env)
        if args[:2] == ("pane", "send-text"):
            return None
        return json.loads(output)["result"]
    with open(directory + "/server.log", "w+") as log:
        server = subprocess.Popen(command + ["server"], env=env, stdout=log, stderr=log)
        try:
            for _ in range(80):
                try:
                    call("workspace", "list")
                    break
                except subprocess.CalledProcessError:
                    time.sleep(.1)
            else:
                raise AssertionError("Server not ready")
            call("plugin", "link", plugin)
            for invocation in range(1, 3):
                call("plugin", "action", "invoke", "workspace.enhancer.open")
                for _ in range(80):
                    logs = call("plugin", "log", "list", "--plugin", "workspace.enhancer")["logs"]
                    assert not any(x["status"] == "failed" for x in logs), logs
                    if sum(x["status"] == "succeeded" and x.get("action_id") == "open" for x in logs) == invocation:
                        break
                    time.sleep(.1)
                else:
                    raise AssertionError("Action did not complete")
            workspaces = call("workspace", "list")["workspaces"]
            assert len(workspaces) == 2 and len({w["workspace_id"] for w in workspaces}) == 2, workspaces
            managers = []
            for workspace in workspaces:
                wid = workspace["workspace_id"]
                tabs = call("tab", "list", "--workspace", wid)["tabs"]
                panes = call("pane", "list", "--workspace", wid)["panes"]
                assert len(tabs) == 1 and tabs[0]["number"] == 1, tabs
                assert len(panes) == 1 and panes[0]["tab_id"] == tabs[0]["tab_id"], panes
                pid = panes[0]["pane_id"]
                managers.append(pid)
                call("pane", "send-text", pid, "r")
                time.sleep(.5)
                screen = subprocess.check_output(command + ["pane", "read", pid, "--source", "visible", "--lines", "40"], env=env, text=True)
                assert "Herdr Workspace Enhancer" in screen, screen
                assert "[active]" not in screen, screen
            project = call("workspace", "create", "--cwd", directory, "--label", "event-created", "--no-focus")["workspace"]
            def wait_text(needle, present):
                for _ in range(60):
                    screens = [subprocess.check_output(command + ["pane", "read", pid, "--source", "visible", "--lines", "40"], env=env, text=True) for pid in managers]
                    if all((needle in screen) == present for screen in screens):
                        return
                    time.sleep(.2)
                raise AssertionError((needle, screens))
            wait_text("event-created", True)
            call("workspace", "rename", project["workspace_id"], "event-renamed")
            wait_text("event-renamed", True)
            call("workspace", "close", project["workspace_id"])
            wait_text("event-renamed", False)
            databases = list(Path(directory).rglob("state.sqlite"))
            assert len(databases) == 1, databases
            with sqlite3.connect(databases[0]) as db:
                db.row_factory = sqlite3.Row
                records = [dict(r) for r in db.execute('SELECT * FROM workspaces WHERE is_manager = 0 ORDER BY rowid')]
            assert len(records) == 1 and records[0]["host_id"] == project["workspace_id"] and records[0]["status"] == "closed", records
            logs = call("plugin", "log", "list", "--plugin", "workspace.enhancer")["logs"]
            assert not any(x["status"] == "failed" for x in logs), logs
            for event in ("workspace.created", "workspace.renamed", "workspace.closed"):
                assert any(x.get("event") == event and x["status"] == "succeeded" for x in logs), logs
            for pid in managers:
                call("pane", "send-text", pid, "q")
            integration_env = dict(env, ENHANCER_INTEGRATION="1",
                                   HERDR_SOCKET_PATH=str(Path(directory) / "config/herdr/sessions" / session / "herdr.sock"))
            subprocess.run(["go", "test", "./service", "-run", "^TestHerdrIntegration$", "-count=1", "-timeout=90s"],
                           cwd=plugin, env=integration_env, check=True)
            print("PASS: two managers; event synchronization; real archive/restore and lost-reply recovery")
        finally:
            subprocess.run(command + ["server", "stop"], env=env, check=False)
            server.wait(timeout=15)
