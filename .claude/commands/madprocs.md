# madprocs Control

You are helping the user control a running madprocs instance - a multi-process development harness with searchable logs.

## Overview

madprocs runs multiple development processes (defined in `mprocs.yaml`) and displays their logs in a TUI. The user has madprocs running in a separate terminal window.

## Port Discovery

**IMPORTANT**: Always check the `.madprocs.port` file first to get the correct URL:

```bash
cat .madprocs.port
```

This file contains the full URL (e.g., `http://localhost:54321`). Use this URL for all API calls.

If the file doesn't exist, madprocs might not be running. Ask the user to start it.

## Web API

First, get the base URL:
```bash
MADPROCS_URL=$(cat .madprocs.port | tr -d '\n')
```

Then use these commands:

### List all processes and their status
```bash
curl -s "${MADPROCS_URL}/api/processes" | jq
```

### Get logs for a specific process
```bash
curl -s "${MADPROCS_URL}/api/logs/PROCESS_NAME" | jq
```

### Start a process
```bash
curl -s -X POST "${MADPROCS_URL}/api/process/PROCESS_NAME/start"
```

### Stop a process
```bash
curl -s -X POST "${MADPROCS_URL}/api/process/PROCESS_NAME/stop"
```

### Restart a process
```bash
curl -s -X POST "${MADPROCS_URL}/api/process/PROCESS_NAME/restart"
```

### Search logs (fetch and grep)
```bash
curl -s "${MADPROCS_URL}/api/logs/PROCESS_NAME" | jq -r '.lines[].content' | grep -i "pattern"
```

## Quick One-Liners

### Check process status
```bash
curl -s "$(cat .madprocs.port | tr -d '\n')/api/processes" | jq '.[] | {name, state}'
```

### Find errors in logs
```bash
curl -s "$(cat .madprocs.port | tr -d '\n')/api/logs/PROCESS_NAME" | jq -r '.lines[].content' | grep -iE 'error|exception|fatal'
```

### Get recent logs (last 50 lines)
```bash
curl -s "$(cat .madprocs.port | tr -d '\n')/api/logs/PROCESS_NAME" | jq -r '.lines[-50:][].content'
```

### Restart a failing process
```bash
curl -s -X POST "$(cat .madprocs.port | tr -d '\n')/api/process/PROCESS_NAME/restart"
```

## TUI Keyboard Shortcuts

If the user needs to interact with the TUI directly, guide them with these shortcuts:

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
| `Esc` | Exit search mode |
| `z` | Toggle fullscreen log view (zoom) |
| `w` | Open web UI in browser |

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

## Tips

1. **Always read `.madprocs.port` first** - This is the only reliable way to get the correct URL
2. **Check process names** - Use `/api/processes` to get the exact process names before other operations
3. **Use jq for parsing** - The API returns JSON, use jq to extract what you need
4. **Web UI for detailed viewing** - For complex log analysis, suggest the user press `w` to open the web UI
