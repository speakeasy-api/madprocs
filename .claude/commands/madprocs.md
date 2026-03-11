# madprocs Control

You are helping the user control a running madprocs instance - a multi-process development harness with searchable logs.

## Overview

madprocs runs multiple development processes (defined in `mprocs.yaml`) and displays their logs in a TUI. The user has madprocs running in a separate terminal window.

**Important**: madprocs exposes a web API that you can use to control processes and fetch logs. The default URL is `http://localhost:3000` but check with the user for the actual port.

## Web API

Use these curl commands to control madprocs programmatically:

### List all processes and their status
```bash
curl -s http://localhost:3000/api/processes | jq
```

### Get logs for a specific process
```bash
curl -s http://localhost:3000/api/logs/PROCESS_NAME | jq
```

### Start a process
```bash
curl -s -X POST http://localhost:3000/api/process/PROCESS_NAME/start
```

### Stop a process
```bash
curl -s -X POST http://localhost:3000/api/process/PROCESS_NAME/stop
```

### Restart a process
```bash
curl -s -X POST http://localhost:3000/api/process/PROCESS_NAME/restart
```

### Search logs (fetch and grep)
```bash
curl -s http://localhost:3000/api/logs/PROCESS_NAME | jq -r '.lines[].content' | grep -i "pattern"
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

## Common Tasks

### Check process status
```bash
curl -s http://localhost:3000/api/processes | jq '.[] | {name, state}'
```

### Find errors in logs
```bash
curl -s http://localhost:3000/api/logs/PROCESS_NAME | jq -r '.lines[].content' | grep -iE 'error|exception|fatal'
```

### Get recent logs (last 50 lines)
```bash
curl -s http://localhost:3000/api/logs/PROCESS_NAME | jq -r '.lines[-50:][].content'
```

### Restart a failing process
```bash
curl -s -X POST http://localhost:3000/api/process/PROCESS_NAME/restart
# Then check if it's running:
curl -s http://localhost:3000/api/processes | jq '.[] | select(.name=="PROCESS_NAME")'
```

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

1. **Always confirm the port** - Ask the user what port madprocs is running on if curl commands fail
2. **Use jq for parsing** - The API returns JSON, use jq to extract what you need
3. **Check process names** - Use `/api/processes` to get the exact process names before other operations
4. **Web UI for detailed viewing** - For complex log analysis, suggest the user press `w` to open the web UI
