# madprocs Control

You are helping the user control a running madprocs instance - a multi-process development harness with searchable logs.

## Overview

madprocs runs multiple development processes (defined in `mprocs.yaml`) and displays their logs in a TUI. The user has madprocs running in a separate terminal window.

## How to Help

When the user asks you to interact with madprocs, guide them with the appropriate keyboard shortcuts or help them with:

1. **Process control** - starting, stopping, restarting processes
2. **Log searching** - finding specific output in logs
3. **Navigation** - switching between processes
4. **Web UI** - opening the browser-based log viewer

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `q` | Quit madprocs |
| `j` / `↓` | Select next process |
| `k` / `↑` | Select previous process |
| `Tab` | Switch focus between process list and logs |
| `s` | Start the selected process |
| `x` | Stop the selected process |
| `r` | Restart the selected process |
| `/` | Search logs (substring match) |
| `?` | Search logs (regex match) |
| `n` | Jump to next search match |
| `N` | Jump to previous search match |
| `Enter` | Next match (while in search mode) |
| `Esc` | Exit search mode |
| `z` | Toggle fullscreen log view (zoom) |
| `w` | Open web UI in browser |

## Search Tips

- **Substring search** (`/`): Case-insensitive, finds exact substrings
- **Regex search** (`?`): Case-insensitive regex patterns (e.g., `error|warn`, `\d{3}`)

## Web UI

The web UI provides:
- Full log history with ANSI color support
- Search and filtering
- Process control (start/stop/restart buttons)
- Log download

Access it by pressing `w` in the TUI, or visit the URL shown in the status bar (default: `http://localhost:3000`).

## Configuration

madprocs uses `mprocs.yaml` (compatible with mprocs). Example:

```yaml
procs:
  server:
    cmd: ["npm", "run", "dev"]
    cwd: ./server

  client:
    shell: "npm start"
    env:
      PORT: "3001"
```

## Common Tasks

### "Find errors in the logs"
Tell the user: Press `/` then type `error` and press Enter. Use `n`/`N` to navigate matches.

### "Restart the API server"
Tell the user: Use `j`/`k` to select the API process, then press `r` to restart it.

### "Stop all processes and quit"
Tell the user: Press `q` to quit - madprocs will gracefully stop all processes.

### "View logs in browser"
Tell the user: Press `w` to open the web UI, which provides a more detailed view with full scrollback.
