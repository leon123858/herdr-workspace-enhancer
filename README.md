# Herdr Workspace Enhancer

A Linux and Windows TUI for workspace notes and archive/restore management. Restoration recreates layouts and shells, not running processes.

## Installation

Install Go 1.22+ and Herdr 0.9.3+. Clone this repository, then run from its directory in a running Herdr session (also works in PowerShell):

```sh
go build -o bin/ ./cmd/herdr-workspace-enhancer
herdr plugin link .
herdr plugin action invoke workspace.enhancer.open
```

Build locally; binary downloads are not provided. Rebuild and relink after updates. Each action invocation opens a new Workspace Enhancer workspace in its first tab.

You can use below command in terminal:

- `herdr plugin action invoke workspace.enhancer.open`
- `herdr plugin action invoke open`

## Keyboard shortcut

Append the following to your Herdr `config.toml`

```toml
[[keys.command]]
key = "prefix+w"
type = "plugin_action"
command = "workspace.enhancer.open"
description = "Open Workspace Enhancer"
```

Then you can trigger plugin UI by **Ctrl+B** + **w**
