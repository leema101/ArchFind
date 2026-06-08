# ArchFind
Fast search for windoze for a defined directory including subdirectory. Works for cloud drives.

# Config
Update config.json with the directory to scan. On running the application an index will be rebuilt from the directory specified. 

# Searches
Search works by using keywords based on the title of the document. the order shown is by most recently modified first. 

# Prerequisites
Install Go

# Build with
go build -o ArchFindTUI.exe

ArchFind (Terminal Edition)
Fast, local file and folder search for large directory trees — built as a terminal UI (TUI) for reliability in VM / Citrix environments.

Overview
ArchFind is a lightweight search tool designed to:

index a large directory structure (e.g. OneDrive, shared folders)
provide instant, keyboard-driven search
run entirely in a terminal window
avoid GUI/OpenGL dependencies (works in constrained environments like Citrix VMs)

The application builds a local index and lets you find files and folders instantly using a responsive, live-updating interface.

Features

🔍 Live search while typing (debounced, smooth)
📂 Searches both files and directories
🧠 Multi-word AND matching (e.g. sip glass)
🔤 Case-insensitive search
📅 Smart sorting:

exact filename match first
then most recently modified


📁 Displays parent folder path
⌨️ Keyboard-driven navigation:

↑ ↓ to move
Enter to open
Esc to clear / exit


⚙️ Config-driven (JSON file next to EXE)
🔄 Automatic background re-indexing (daily refresh)
🚫 No OpenGL / no desktop UI dependencies


# Prerequisites
1) Go installed
You need the Go toolchain installed and available on your system PATH.

Recommended: latest stable version of Go
Verify with:

Shellgo versionShow more lines
Go uses modules (go.mod) as its dependency system. [go.dev]

2) Go modules support
This project uses Go modules for dependency management (standard in modern Go).
The Go tool will automatically download dependencies on first build. [index.golang.org]

3) Terminal environment
The app runs fully in a terminal using:

tview (terminal UI widgets) [pkg.go.dev]
tcell (terminal input/output handling) [pkg.go.dev]

Any of the following work:

Command Prompt
PowerShell
Windows Terminal
Citrix-provided shell


4) No external native dependencies
This project is pure Go:

✅ No GCC / MinGW required
✅ No CGO
✅ No OpenGL
✅ No GUI frameworks





## Configuration

# Config file

{
  "root_paths": [
    "G:\\My Drive\\",
    "D:\\Projects\\",
    "C:\\Work\\docs"
  ],
  "index_path": "archfind-index.json",
  "max_results": 20,
  "exclude_temp_files": true,
  "fuzzy_enabled": false,
  "fuzzy_max_distance": 1,
  "search_debounce_ms": 300
}

# Config Fields

Field               Description
root_path           Root directory to index
index_path          Where the index file is stored
max_results         Number of results shown
exclude_temp_files  Skip temp files (~$, .tmp, etc.)
fuzzy_enabled       Enable fuzzy matching
fuzzy_max_distance  Fuzzy match tolerance
search_debounce_ms  Delay before search triggers

# Usage
Run the executable:
ArchFindTUI.exe



# Controls

Key         Action
Type        Update search
↑ / ↓       Move selection
Enter       Open file/folder
Backspace   Delete
Esc         Clear search / exit



























KeyActionTypeUpdate search↑ / ↓Move selectionEnterOpen file/folderBackspaceDeleteEscClear search / exit
Opening behavior

Files → open in default Windows app
Folders → open in Explorer


First Run

If no index exists → the tool builds it first
If an index exists → it loads instantly
If index is older than today → background rebuild starts


Architecture
The application consists of:

Indexer

walks filesystem
builds JSON index


Search engine

precomputes normalized names
fast in-memory filtering


TUI layer

keyboard input loop
result rendering


Background worker

daily index refresh




Why Terminal UI?
Designed specifically to work in constrained environments:

✅ Citrix / VDI
✅ Remote desktop sessions
✅ Systems without OpenGL support
✅ Locked-down enterprise environments

tview provides a rich interface without needing a graphical desktop. [pkg.go.dev]

Known Limitations

Windows-oriented (Explorer / file opening)
Not a system-wide search index (per configured root only)
Large initial index creation may take time


Future Enhancements

Match highlighting in results
Improved ranking/scoring
Recent file weighting
Keyboard shortcut launcher integration
Cross-platform open support


License
MIT 

Author
Built as a lightweight, enterprise-friendly search tool for fast local discovery.

Quick Start (TL;DR)
go mod tidy
go build -o ArchFindTUi.exe
ArchFindTUI.exe

