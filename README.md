# review

A lightweight, self-contained web-based code review tool. Browse a project's source files, add inline annotations to specific lines, and have everything persisted to a `REVIEW.md` markdown file.

The tool was vibecoded as a simple way to review agentic coded files. The markdown file created by this tool can be fed back to your coding agent.

## Features

- **File tree navigation** — browse the project with expandable directories
- **Inline annotations** — click any line to add, edit, or delete review comments
- **Syntax highlighting** — powered by [Chroma](https://github.com/alecthomas/chroma)
- **Git status integration** — files and directories are color-coded by git status (modified, staged, untracked, etc.)
- **Git diff markers** — changed/added lines are marked in the gutter; hover to see the full diff hunk
- **Scrollbar annotations** — colored markers on the scrollbar show where comments and changes are in long files
- **Live updates** — files reload automatically when changed on disk via WebSocket-based file watching
- **Drift detection** — annotations automatically relocate when code moves, or are marked outdated if context is lost
- **New Review** — start a fresh review with one click, clearing all existing annotations
- **Graceful shutdown** — the browser tab closes automatically when the server stops
- **Markdown storage** — all annotations are saved to `REVIEW.md` with surrounding code context, making them easy to read and share without the tool
- **Single binary** — the web frontend is embedded in the Go binary; no external files or dependencies needed at runtime

## Building

Requires **Go 1.24+**.

```sh
go build -o review .
```

or

```sh
make
```

## Usage

```sh
# Review the current directory on the default port (7070)
./review

# Review a specific directory on a custom port
./review -dir /path/to/project -port 8080
```

Then open `http://localhost:7070` (or your chosen port) in a browser (should happen automatically).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | `.` | Root directory of the project to review |
| `-port` | `7070` | HTTP server port |

## How It Works

The tool serves a three-panel web UI:

1. **File tree** (left) — project files with git status indicators and comment markers
2. **Code viewer** (center) — syntax-highlighted source with clickable lines
3. **Comment sidebar** (right) — list of annotations for the current file and an editor

Annotations are stored in memory and flushed to `REVIEW.md` in the project root on every change. The markdown file groups comments by file and includes a few lines of code context around each annotated line, so it remains useful on its own.

## API

The tool exposes a JSON API for the frontend:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/tree` | File tree structure |
| `GET` | `/api/file?path=<path>` | Syntax-highlighted file content with diff data |
| `GET` | `/api/annotations?path=<path>` | Annotations (all or per-file) |
| `POST` | `/api/annotations` | Create or update an annotation |
| `DELETE` | `/api/annotations` | Delete an annotation |
| `GET` | `/api/git-status` | Git status for all files |
| `DELETE` | `/api/review` | Delete REVIEW.md and start a new review |
| `GET` | `/ws` | WebSocket for live updates |

## License

MIT
