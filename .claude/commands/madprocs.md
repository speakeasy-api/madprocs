# madprocs Control

You are helping the user control a running madprocs instance - a multi-process development harness with searchable logs.

## CLI Commands

Use these commands to control madprocs. They communicate with the running instance automatically.

### Check status of all processes
```bash
madprocs status
```

### View logs for a process
```bash
madprocs logs <process-name>
```

### Start a process
```bash
madprocs start <process-name>
```

### Stop a process
```bash
madprocs stop <process-name>
```

### Restart a process
```bash
madprocs restart <process-name>
```

## Common Tasks

### Check what's running
```bash
madprocs status
```

### Find errors in a process's logs
```bash
madprocs logs server | grep -iE 'error|exception|fatal'
```

### Get recent logs (last 50 lines)
```bash
madprocs logs server | tail -50
```

### Restart a failing process
```bash
madprocs restart server
madprocs status
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

1. **Always run `madprocs status` first** - This shows all processes and their current state
2. **Use grep to search logs** - `madprocs logs <name> | grep "pattern"`
3. **Web UI for detailed viewing** - For complex log analysis, suggest the user press `w` to open the web UI
