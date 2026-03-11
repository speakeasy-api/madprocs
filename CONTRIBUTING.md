# Contributing to madprocs

## Development Setup

```bash
# Clone the repo
git clone https://github.com/speakeasy-api/madprocs.git
cd madprocs

# Build
go build -o madprocs .

# Run with a test config
./madprocs -c mprocs.yaml
```

## Project Structure

```
madprocs/
├── main.go              # Entry point, CLI flags
├── config/              # YAML config parsing
├── process/             # Process lifecycle management
├── log/                 # Searchable log ring buffer
├── ui/                  # Bubbletea TUI components
└── web/                 # Embedded web server
    └── static/          # HTML/CSS/JS for web UI
```

## Making Changes

1. Fork the repo and create a branch
2. Make your changes
3. Test locally with `go build && ./madprocs`
4. Run `go vet ./...` and `go fmt ./...`
5. Submit a PR

## Releasing

Releases are automated via GitHub Actions. To create a release:

```bash
# Patch release (0.0.x)
./scripts/release.sh patch

# Minor release (0.x.0)
./scripts/release.sh minor

# Major release (x.0.0)
./scripts/release.sh major
```

The script will:
1. Bump the version
2. Create and push a git tag
3. Wait for GitHub Actions to build the release

## Code Style

- Follow standard Go conventions
- Use `go fmt` for formatting
- Keep functions focused and small
- Handle errors explicitly

## Web UI

The web UI is embedded in the binary using `//go:embed`. When modifying files in `web/static/`, rebuild to see changes.

## Testing

```bash
go test ./...
```
