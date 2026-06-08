# ArchFind

A fast, keyboard-driven file and folder search TUI for Windows, built with Go and [tview](https://github.com/rivo/tview).

ArchFind pre-indexes one or more directory trees and lets you search them instantly from a terminal — no shell, no file explorer, just type and open.

---

## Features

- **Instant search** — results appear as you type via a debounced live search against a pre-built index
- **Multiple root directories** — scan and search across several folder trees simultaneously; configure them as a JSON array
- **Overlap detection** — if two configured paths are nested (e.g. `D:\Docs` and `D:\Docs\Archive`), the child is skipped automatically so files are never indexed twice
- **Auto re-index on config change** — if `config.json` is newer than the index at startup, the index is rebuilt immediately before the UI opens
- **Daily background rebuild** — if the index is from a previous calendar day, a silent background rebuild runs while you search; the UI updates automatically when it completes
- **Fuzzy search** — optional Levenshtein-distance matching catches near-miss spellings (configurable distance)
- **File and folder results** — results are labelled `[FILE]` or `[DIR ]` and sorted by most recently modified first
- **One-key open** — press Enter to open the selected file with its default application, or open a folder in Explorer
- **Keyboard navigation** — `↑`/`↓` move through results without leaving the search field; `Esc` clears the query
- **Temp file exclusion** — files starting with `~$` or ending in `.tmp`/`.temp` are skipped during indexing
- **Duplicate-input suppression** — handles spurious duplicate key events from slow VDI/RDP environments; the window is tunable via `dedup_window_ms`
- **Debug input log** — set `ARCHFIND_DEBUG_INPUT=1` to write a timestamped key-event log next to the executable for diagnosing input issues
- **Works with cloud drives** — any path accessible as a local folder (OneDrive, Google Drive, etc.) can be indexed

---

## Requirements

- Windows 10 or Windows 11
- A terminal that supports [tcell](https://github.com/gdamore/tcell) (Windows Console, Windows Terminal, ConPTY)

---

## Installation

1. Download `ArchFindTUI.exe` and place it in any folder.
2. Run it once — `config.json` will be created automatically in the same folder.
3. Edit `config.json` to set your root paths (see below).
4. Run again — the index will be built and the UI will open.

---

## Configuration (`config.json`)

```json
{
  "root_paths": [
    "D:\\Documents",
    "E:\\Projects"
  ],
  "index_path": "archfind-index.json",
  "max_results": 20,
  "exclude_temp_files": true,
  "fuzzy_enabled": false,
  "fuzzy_max_distance": 1,
  "search_debounce_ms": 300,
  "dedup_window_ms": 150
}
```

| Key | Type | Description |
|---|---|---|
| `root_paths` | array of strings | Directories to index. Sub-paths of each other are deduplicated automatically. |
| `root_path` | string | Legacy single-directory alternative to `root_paths`. Ignored if `root_paths` is present. |
| `index_path` | string | Path to the index file. Relative paths are resolved next to the executable. |
| `max_results` | int | Maximum number of results shown (default `20`). |
| `exclude_temp_files` | bool | Skip `~$*`, `*.tmp`, and `*.temp` files during indexing (default `true`). |
| `fuzzy_enabled` | bool | Enable fuzzy (approximate) matching (default `false`). |
| `fuzzy_max_distance` | int | Maximum Levenshtein edit distance for fuzzy matches (default `1`). |
| `search_debounce_ms` | int | Milliseconds to wait after the last keystroke before running the search (default `300`). |
| `dedup_window_ms` | int | Milliseconds within which an identical key event is suppressed as a duplicate. Increase (e.g. `200`) on slow VDI/RDP connections (default `150`). |

> **Note:** In JSON, Windows paths must use double backslashes: `"D:\\My Folder"`.

---

## Keyboard Shortcuts

| Key | Action |
|---|---|
| Type | Filter results live |
| `↑` / `↓` | Move selection up / down |
| `Enter` | Open selected file or folder |
| `Esc` | Clear the search field |

---

## How indexing works

On first launch (or when no valid index is found), ArchFind builds the index synchronously before opening the UI. Subsequent launches load the cached index instantly and trigger a background rebuild only if:

- The index is from a previous calendar day, **or**
- `config.json` has been modified since the index was last built.

The index is stored as a JSON file (`archfind-index.json` by default) next to the executable.

---

## Building from source

```sh
git clone <repo-url>
cd archfind
go build -o ArchFindTUI.exe .
```

Requires Go 1.21 or later.

---

## License

MIT
