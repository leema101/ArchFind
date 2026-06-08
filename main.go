package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Config struct {
	RootPath         string   `json:"root_path"`
	RootPaths        []string `json:"root_paths"`
	IndexPath        string   `json:"index_path"`
	MaxResults       int      `json:"max_results"`
	ExcludeTempFiles bool     `json:"exclude_temp_files"`
	FuzzyEnabled     bool     `json:"fuzzy_enabled"`
	FuzzyMaxDistance int      `json:"fuzzy_max_distance"`
	SearchDebounceMs int      `json:"search_debounce_ms"`
}

// effectiveRootPaths returns the unified list of directories to scan.
// root_paths takes precedence; root_path is the legacy fallback.
func (c Config) effectiveRootPaths() []string {
	if len(c.RootPaths) > 0 {
		return c.RootPaths
	}
	if c.RootPath != "" {
		return []string{c.RootPath}
	}
	return nil
}

type ItemType string

const (
	ItemFile   ItemType = "file"
	ItemFolder ItemType = "folder"
)

type IndexItem struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Parent  string   `json:"parent"`
	ModUnix int64    `json:"mod_unix"`
	Type    ItemType `json:"type"`
}

type PreparedItem struct {
	Item     IndexItem
	NormName string
	Tokens   []string
}

type IndexFile struct {
	BuiltAtUnix int64       `json:"built_at_unix"`
	RootPath    string      `json:"root_path"`
	Items       []IndexItem `json:"items"`
}

type Engine struct {
	mu       sync.RWMutex
	index    IndexFile
	prepared []PreparedItem
	cfg      Config
}

type RebuildState struct {
	mu      sync.RWMutex
	running bool
	lastMsg string
}

func main() {
	cfg, exeDir, err := loadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Optional debug log for input events. Enable by setting env var
	// ARCHFIND_DEBUG_INPUT=1. Log file will be created next to the exe.
	var debugLog *os.File
	if os.Getenv("ARCHFIND_DEBUG_INPUT") != "" {
		dbgPath := filepath.Join(exeDir, "archfind-input.log")
		if f, err := os.Create(dbgPath); err == nil {
			debugLog = f
			fmt.Printf("Input debug log: %s\n", dbgPath)
			defer debugLog.Close()
		} else {
			fmt.Printf("Failed to create debug log: %v\n", err)
		}
	}
	indexPath := resolveIndexPath(cfg.IndexPath, exeDir)

	var idx IndexFile
	loaded := false
	if fileExists(indexPath) {
		idx, err = loadIndex(indexPath)
		if err == nil {
			loaded = true
		}
	}

	engine := &Engine{cfg: cfg}
	engine.ReplaceIndex(idx)

	// First run: if no index exists, build synchronously before starting the TUI.
	if !loaded || len(idx.Items) == 0 {
		fmt.Println("No usable index found. Building initial index, please wait...")
		newIdx, err := buildIndex(cfg)
		if err != nil {
			fmt.Printf("Error building initial index: %v\n", err)
			return
		}
		if err := saveIndex(indexPath, newIdx); err != nil {
			fmt.Printf("Error saving initial index: %v\n", err)
			return
		}
		engine.ReplaceIndex(newIdx)
		idx = newIdx
		loaded = true
	}

	rebuild := &RebuildState{}

	app := tview.NewApplication()

	title := tview.NewTextView().
		SetText(" ArchFind ").
		SetDynamicColors(true)

	search := tview.NewInputField().
		SetLabel("Search: ").
		SetFieldWidth(0)

	resultsList := tview.NewList().
		ShowSecondaryText(true)
	resultsList.SetBorder(true).SetTitle(" Results ")

	status := tview.NewTextView().
		SetDynamicColors(true)
	status.SetBorder(true).SetTitle(" Status ")

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(title, 1, 0, false).
		AddItem(search, 1, 0, true).
		AddItem(resultsList, 0, 1, false).
		AddItem(status, 3, 0, false)

	var results []IndexItem
	selected := 0

	updateStatus := func(query string) {
		engine.mu.RLock()
		total := len(engine.index.Items)
		builtAt := engine.index.BuiltAtUnix
		engine.mu.RUnlock()

		builtText := "never"
		if builtAt > 0 {
			builtText = time.Unix(builtAt, 0).Format("2006-01-02 15:04")
		}

		rebuild.mu.RLock()
		rebuildRunning := rebuild.running
		rebuildMsg := rebuild.lastMsg
		rebuild.mu.RUnlock()

		line1 := fmt.Sprintf("[yellow]Items:[white] %d   [yellow]Built:[white] %s", total, builtText)
		line2 := "[yellow]Keys:[white] Type=search   ↑/↓=move   Enter=open   Esc=clear"

		if rebuildRunning || rebuildMsg != "" {
			line3 := fmt.Sprintf("[yellow]Rebuild:[white] %s", rebuildMsg)
			status.SetText(line1 + "\n" + line2 + "\n" + line3)
		} else {
			status.SetText(line1 + "\n" + line2)
		}
	}

	refreshResults := func(query string, found []IndexItem) {
		results = found

		if len(results) == 0 {
			selected = -1
		} else if selected < 0 || selected >= len(results) {
			selected = 0
		}

		resultsList.Clear()

		if len(results) == 0 {
			resultsList.AddItem("No matches", "", 0, nil)
			updateStatus(query)
			return
		}

		for _, item := range results {
			prefix := "[FILE]"
			if item.Type == ItemFolder {
				prefix = "[DIR ]"
			}
			mainText := fmt.Sprintf("%s %s", prefix, item.Name)
			secondaryText := item.Parent
			resultsList.AddItem(mainText, secondaryText, 0, nil)
		}

		if selected >= 0 && selected < len(results) {
			resultsList.SetCurrentItem(selected)
		}

		updateStatus(query)
	}

	openSelected := func() {
		if selected < 0 || selected >= len(results) {
			return
		}
		if err := openItem(results[selected]); err != nil {
			status.SetText(status.GetText(true) + "\n[red]Open failed:[white] " + err.Error())
		}
	}

	// Debounced live search
	var debounceMu sync.Mutex
	var debounceTimer *time.Timer

	var lastRune rune
	var lastRuneModifiers tcell.ModMask
	var lastRuneTime time.Time

	scheduleSearch := func(query string) {
		debounceMu.Lock()
		defer debounceMu.Unlock()

		if debounceTimer != nil {
			debounceTimer.Stop()
		}

		delay := time.Duration(cfg.SearchDebounceMs) * time.Millisecond

		debounceTimer = time.AfterFunc(delay, func() {
			found := engine.Search(query)

			app.QueueUpdateDraw(func() {
				// Ignore stale results if user kept typing.
				if search.GetText() != query {
					return
				}
				selected = 0
				refreshResults(query, found)
			})
		})
	}

	search.SetChangedFunc(func(text string) {
		if debugLog != nil {
			fmt.Fprintf(debugLog, "%s\tchanged\ttext=%q\n", time.Now().Format(time.RFC3339Nano), text)
			_ = debugLog.Sync()
		}
		scheduleSearch(text)
	})

	resultsList.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		selected = index
		openSelected()
	})

	search.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if debugLog != nil {
			// Note: EventKey.Rune() is valid only for KeyRune events.
			r := rune(0)
			if event.Key() == tcell.KeyRune {
				r = event.Rune()
			}
			fmt.Fprintf(debugLog, "%s\tcapture\tkey=%v\trune=%q\tmod=%v\n", time.Now().Format(time.RFC3339Nano), event.Key(), r, event.Modifiers())
			_ = debugLog.Sync()
		}

		if event.Key() == tcell.KeyRune {
			now := time.Now()
			// Suppress OS-level duplicate key events (a known tcell/Windows
			// console issue where the same keypress is delivered twice).
			// True duplicates arrive within a few milliseconds; deliberate
			// repeated characters (e.g. "ss") are typed 50ms+ apart, so
			// 25ms is safe.
			if event.Rune() == lastRune && event.Modifiers() == lastRuneModifiers && !lastRuneTime.IsZero() {
				if now.Sub(lastRuneTime) < 25*time.Millisecond {
					cur := search.GetText()
					runes := []rune(cur)
					if len(runes) > 0 && runes[len(runes)-1] == event.Rune() {
						if debugLog != nil {
							fmt.Fprintf(debugLog, "%s\tsuppressed_duplicate\trune=%q\tdelta=%v\n", time.Now().Format(time.RFC3339Nano), event.Rune(), now.Sub(lastRuneTime))
							_ = debugLog.Sync()
						}
						return nil
					}
				}
			}
			lastRune = event.Rune()
			lastRuneModifiers = event.Modifiers()
			lastRuneTime = now
		} else {
			lastRuneTime = time.Time{}
		}

		switch event.Key() {
		case tcell.KeyUp:
			if len(results) > 0 && selected > 0 {
				selected--
				resultsList.SetCurrentItem(selected)
			}
			return nil

		case tcell.KeyDown:
			if len(results) > 0 && selected < len(results)-1 {
				selected++
				resultsList.SetCurrentItem(selected)
			}
			return nil

		case tcell.KeyEnter:
			openSelected()
			return nil

		case tcell.KeyEscape:
			if search.GetText() != "" {
				search.SetText("")
				selected = 0
				refreshResults("", engine.Search(""))
				return nil
			}
			//app.Stop()
			return nil
		}

		return event
	})

	// Prime UI
	refreshResults("", engine.Search(""))
	updateStatus("")

	// Background rebuild if index is stale
	go func() {
		engine.mu.RLock()
		currentBuiltAt := engine.index.BuiltAtUnix
		engine.mu.RUnlock()

		shouldRebuild := !sameLocalDate(time.Unix(currentBuiltAt, 0), time.Now())
		if !shouldRebuild {
			return
		}

		rebuild.Set(true, "Rebuilding index in background...")
		app.QueueUpdateDraw(func() {
			updateStatus(search.GetText())
		})

		start := time.Now()
		newIdx, err := buildIndex(cfg)
		if err != nil {
			rebuild.Set(false, "Rebuild failed: "+err.Error())
			app.QueueUpdateDraw(func() {
				updateStatus(search.GetText())
			})
			return
		}

		if err := saveIndex(indexPath, newIdx); err != nil {
			rebuild.Set(false, "Rebuild completed, but save failed: "+err.Error())
			app.QueueUpdateDraw(func() {
				updateStatus(search.GetText())
			})
			return
		}

		engine.ReplaceIndex(newIdx)
		rebuild.Set(false, fmt.Sprintf("Complete: %d items in %s", len(newIdx.Items), time.Since(start).Round(time.Second)))

		app.QueueUpdateDraw(func() {
			currentQuery := search.GetText()
			refreshResults(currentQuery, engine.Search(currentQuery))
			updateStatus(currentQuery)
		})
	}()

	if err := app.SetRoot(layout, true).SetFocus(search).Run(); err != nil {
		fmt.Printf("Application error: %v\n", err)
	}
}

func prepareItems(items []IndexItem) []PreparedItem {
	out := make([]PreparedItem, 0, len(items))
	for _, it := range items {
		norm := normalize(it.Name)
		out = append(out, PreparedItem{
			Item:     it,
			NormName: norm,
			Tokens:   tokenizeName(norm),
		})
	}
	return out
}

func (e *Engine) ReplaceIndex(idx IndexFile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.index = idx
	e.prepared = prepareItems(idx.Items)
}

func (e *Engine) Search(query string) []IndexItem {
	e.mu.RLock()
	defer e.mu.RUnlock()

	query = normalize(query)

	if query == "" {
		items := make([]IndexItem, 0, len(e.prepared))
		for _, p := range e.prepared {
			items = append(items, p.Item)
		}
		sortResults(items, "")
		if len(items) > e.cfg.MaxResults {
			items = items[:e.cfg.MaxResults]
		}
		return items
	}

	terms := splitTerms(query)
	if len(terms) == 0 {
		return nil
	}

	matches := make([]IndexItem, 0, e.cfg.MaxResults*4)
	for _, p := range e.prepared {
		if matchesPrepared(p, terms, e.cfg) {
			matches = append(matches, p.Item)
		}
	}

	sortResults(matches, query)

	if len(matches) > e.cfg.MaxResults {
		matches = matches[:e.cfg.MaxResults]
	}
	return matches
}

func matchesPrepared(p PreparedItem, terms []string, cfg Config) bool {
	for _, term := range terms {
		if strings.Contains(p.NormName, term) {
			continue
		}
		if cfg.FuzzyEnabled && fuzzyMatchTerm(term, p.Tokens, cfg.FuzzyMaxDistance) {
			continue
		}
		return false
	}
	return true
}

func sortResults(items []IndexItem, query string) {
	query = normalize(query)

	sort.Slice(items, func(i, j int) bool {
		a := items[i]
		b := items[j]

		aExact := normalize(a.Name) == query
		bExact := normalize(b.Name) == query

		if aExact != bExact {
			return aExact
		}
		if a.ModUnix != b.ModUnix {
			return a.ModUnix > b.ModUnix
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

var nonWord = regexp.MustCompile(`[^\pL\pN]+`)

func splitTerms(s string) []string {
	return strings.Fields(strings.TrimSpace(s))
}

func tokenizeName(name string) []string {
	parts := nonWord.Split(strings.ToLower(name), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fuzzyMatchTerm(term string, tokens []string, maxDistance int) bool {
	for _, tok := range tokens {
		if abs(len([]rune(tok))-len([]rune(term))) > maxDistance {
			continue
		}
		if levenshtein(tok, term) <= maxDistance {
			return true
		}
	}
	return false
}

func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)

	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	dp := make([][]int, len(ar)+1)
	for i := range dp {
		dp[i] = make([]int, len(br)+1)
	}

	for i := 0; i <= len(ar); i++ {
		dp[i][0] = i
	}
	for j := 0; j <= len(br); j++ {
		dp[0][j] = j
	}

	for i := 1; i <= len(ar); i++ {
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			dp[i][j] = min3(
				dp[i-1][j]+1,
				dp[i][j-1]+1,
				dp[i-1][j-1]+cost,
			)
		}
	}

	return dp[len(ar)][len(br)]
}

func openItem(item IndexItem) error {
	if item.Type == ItemFolder {
		return exec.Command("explorer.exe", item.Path).Start()
	}
	return exec.Command("cmd", "/c", "start", "", item.Path).Start()
}

func loadConfig() (Config, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return Config{}, "", err
	}
	exeDir := filepath.Dir(exePath)
	cfgPath := filepath.Join(exeDir, "config.json")

	if !fileExists(cfgPath) {
		defaultCfg := Config{
			RootPaths:        []string{`C:\OneDrive\OneDrive - Standard Bank\S\architecture`},
			IndexPath:        "archfind-index.json",
			MaxResults:       20,
			ExcludeTempFiles: true,
			FuzzyEnabled:     false,
			FuzzyMaxDistance: 1,
			SearchDebounceMs: 300,
		}
		b, _ := json.MarshalIndent(defaultCfg, "", "  ")
		_ = os.WriteFile(cfgPath, b, 0644)
		return Config{}, "", fmt.Errorf("config.json was created at %s - review it and run again", cfgPath)
	}

	var cfg Config
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return Config{}, "", err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, "", err
	}

	if len(cfg.effectiveRootPaths()) == 0 {
		return Config{}, "", errors.New("config.json must specify root_paths (array) or root_path (string)")
	}
	if strings.TrimSpace(cfg.IndexPath) == "" {
		cfg.IndexPath = "archfind-index.json"
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 20
	}
	if cfg.FuzzyMaxDistance <= 0 {
		cfg.FuzzyMaxDistance = 1
	}
	if cfg.SearchDebounceMs <= 0 {
		cfg.SearchDebounceMs = 300
	}

	return cfg, exeDir, nil
}

func buildIndex(cfg Config) (IndexFile, error) {
	roots := cfg.effectiveRootPaths()

	var missing []string
	for _, r := range roots {
		if !fileExists(r) {
			missing = append(missing, r)
		}
	}
	if len(missing) == len(roots) {
		return IndexFile{}, fmt.Errorf("none of the configured root paths exist: %s", strings.Join(missing, ", "))
	}

	items := make([]IndexItem, 0, 32000)

	for _, root := range roots {
		if !fileExists(root) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if path == root {
				return nil
			}

			name := d.Name()
			if shouldExclude(name, cfg) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			itemType := ItemFile
			if d.IsDir() {
				itemType = ItemFolder
			}

			items = append(items, IndexItem{
				Name:    name,
				Path:    path,
				Parent:  filepath.Dir(path),
				ModUnix: info.ModTime().Unix(),
				Type:    itemType,
			})

			return nil
		})
		if err != nil {
			return IndexFile{}, fmt.Errorf("error walking %s: %w", root, err)
		}
	}

	return IndexFile{
		BuiltAtUnix: time.Now().Unix(),
		RootPath:    strings.Join(roots, "; "),
		Items:       items,
	}, nil
}

func shouldExclude(name string, cfg Config) bool {
	if !cfg.ExcludeTempFiles {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "~$") {
		return true
	}
	if strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".temp") {
		return true
	}
	return false
}

func saveIndex(path string, idx IndexFile) error {
	tmp := path + ".tmp"
	b, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadIndex(path string) (IndexFile, error) {
	var idx IndexFile
	b, err := os.ReadFile(path)
	if err != nil {
		return idx, err
	}
	err = json.Unmarshal(b, &idx)
	return idx, err
}

func resolveIndexPath(indexPath string, exeDir string) string {
	if filepath.IsAbs(indexPath) {
		return indexPath
	}
	return filepath.Join(exeDir, indexPath)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sameLocalDate(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (r *RebuildState) Set(running bool, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
	r.lastMsg = msg
}
