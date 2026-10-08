# review

A lightweight, self-contained web-based code review tool. Browse a project's source files, add inline annotations to specific lines, and have everything persisted to a `REVIEW.md` markdown file.

The tool was vibecoded as a simple way to review agentic coded files. The markdown file created by this tool can be fed back to your coding agent.

## Features

- **File tree navigation** — browse the project with expandable directories
- **Inline annotations** — click any line to add, edit, or delete review comments
- **Syntax highlighting** — powered by [Chroma](https://github.com/alecthomas/chroma)
- **Git status integration** — files and directories are color-coded by git status (modified, staged, untracked, etc.)
- **Git diff markers** — changed, added, and deleted lines are marked in the gutter; hover to see the full diff hunk
- **Compare against a base** — review an already committed branch by diffing it against a branch, tag, or commit
- **Scrollbar annotations** — colored markers on the scrollbar show where comments and changes are in long files
- **Live updates** — files reload automatically when changed on disk via WebSocket-based file watching
- **Drift detection** — annotations automatically relocate when code moves, or are marked outdated if context is lost
- **New Review** — start a fresh review with one click, clearing all existing annotations
- **Review history** — optionally keep retired reviews in `.review-history/` instead of deleting them, each named after its last change and kept out of git
- **Graceful shutdown** — the browser tab closes automatically when the server stops
- **Markdown storage** — all annotations are saved to `REVIEW.md` with surrounding code context and a header naming the commit the review was taken from, making them easy to read and share without the tool
- **Single binary** — the web frontend is embedded in the Go binary; no external files or dependencies needed at runtime

## Building

Requires **Go 1.25+**.

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

# Review a branch that is already committed, comparing it against main
./review main

# Enable review history for a project (keeps retired reviews)
./review setup-review-history
```

Then open `http://127.0.0.1:7070` (or your chosen port) in a browser (should happen automatically).

The server listens on the loopback interface only, so the reviewed sources are not reachable from other machines.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | `.` | Root directory of the project to review |
| `-port` | `7070` | HTTP server port |

### Comparing Against a Base Revision

By default, files and lines are highlighted by their working tree changes, so nothing stands out once the work is committed. Pass a branch, tag or commit ID as argument to review committed work instead:

```sh
./review main
./review v1.2.0
./review 8f3a91c
```

Highlighting and diff hunks then cover everything that changed between that revision and the current working tree, committed or not. The comparison starts at the merge base of the given revision and `HEAD`, so commits made on the base branch after branching off are not shown as changes. The active base is displayed in the status bar.

### Review History

By default **New Review** deletes `REVIEW.md` and its comments with it. Enabling review history keeps them instead:

```sh
./review setup-review-history
```

That creates a `.review-history/` directory beside the review file, with an `AGENTS.md` in it asking agents to ignore the retired reviews unless asked, and adds `/REVIEW.md` and `/.review-history/` to the project's `.gitignore`, keeping both the current review and the retired ones out of the project's commits.

From then on, starting a new review moves the old one into that directory as `REVIEW-<time>.md`, where the time is the file's last change — a copy of the review as it stood is never lost. The confirmation prompt names the file the current review will become.

## How It Works

The tool serves a three-panel web UI:

1. **File tree** (left) — project files with git status indicators and comment markers
2. **Code viewer** (center) — syntax-highlighted source with clickable lines
3. **Comment sidebar** (right) — list of annotations for the current file and an editor

Annotations are stored in memory and flushed to `REVIEW.md` in the project root on every change. The markdown file groups comments by file and includes a few lines of code context around each annotated line, so it remains useful on its own.

Its header records where the review was taken from and when it was made, so a review file stays meaningful on its own:

```markdown
# Code Review

_Base: c398eeeb9566f399076658bc3c21e698e1ed1c43 (main) — add main and util [dirty changeset]_
_Started: 2026-10-08_
_Created: 2026-10-08T10:06:11Z_
_Modified: 2026-10-08T10:06:18Z_
```

`_Base` names the commit the review is measured against — the revision given on the command line, or `HEAD` — with the first line of its message, and `[dirty changeset]` when the working tree has changes beyond it. `_Created` and `_Modified` are UTC, and the base follows a new revision whenever the review is opened against a different one. Existing `_Started` dates are kept.

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
| `GET` | `/api/config` | Review settings, currently the base revision and whether review history is enabled |
| `DELETE` | `/api/review` | Start a new review; delete REVIEW.md, or move it to the review history |
| `GET` | `/ws` | WebSocket for live updates |

## License

MIT
