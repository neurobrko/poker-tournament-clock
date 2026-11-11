package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type browserPurpose int

const (
	browseTournamentDB browserPurpose = iota
	browseTemplate
	browseSaveDatabase
	browseSaveTemplate
	browseLevelSound
	browseMinuteSound
	browsePlayers
	browseSavePlayers
	browseEditorTemplate
	browseEditorSaveTemplate
)

type browserEntry struct {
	name       string
	path       string
	dir        bool
	selectable bool
}

type fileBrowser struct {
	purpose         browserPurpose
	directory       string
	entries         []browserEntry
	cursor          int
	showHidden      bool
	showUnsupported bool
}

func newFileBrowser(purpose browserPurpose, directory string) (*fileBrowser, error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	browser := &fileBrowser{purpose: purpose, directory: absolute}
	if err := browser.reload(); err != nil {
		return nil, err
	}
	return browser, nil
}

func (b *fileBrowser) reload() error {
	items, err := os.ReadDir(b.directory)
	if err != nil {
		return fmt.Errorf("read directory %s: %w", b.directory, err)
	}
	entries := []browserEntry{{name: "..", path: filepath.Dir(b.directory), dir: true}}
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") && !b.showHidden {
			continue
		}
		path := filepath.Join(b.directory, name)
		isDir := item.IsDir()
		if !isDir && item.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(path); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			entries = append(entries, browserEntry{name: name + string(os.PathSeparator), path: path, dir: true, selectable: true})
			continue
		}
		selectable := b.matches(name) && b.purpose != browseSaveDatabase && b.purpose != browseSaveTemplate && b.purpose != browseSavePlayers && b.purpose != browseEditorSaveTemplate
		if !selectable && !b.showUnsupported {
			continue
		}
		entries = append(entries, browserEntry{name: name, path: path, selectable: selectable})
	}
	sort.SliceStable(entries[1:], func(i, j int) bool {
		left, right := entries[i+1], entries[j+1]
		if left.dir != right.dir {
			return left.dir
		}
		return strings.ToLower(left.name) < strings.ToLower(right.name)
	})
	b.entries = entries
	if b.cursor >= len(entries) {
		b.cursor = len(entries) - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	return nil
}

func (b *fileBrowser) toggleHidden() error {
	b.showHidden = !b.showHidden
	return b.reload()
}

func (b *fileBrowser) toggleUnsupported() error {
	b.showUnsupported = !b.showUnsupported
	return b.reload()
}

func (b *fileBrowser) matches(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch b.purpose {
	case browseTemplate, browseSaveTemplate, browsePlayers, browseSavePlayers, browseEditorTemplate, browseEditorSaveTemplate:
		return ext == ".yaml" || ext == ".yml"
	case browseTournamentDB:
		return ext == ".sqlite" || ext == ".sqlite3" || ext == ".db"
	case browseLevelSound, browseMinuteSound:
		return ext == ".wav" || ext == ".mp3"
	default:
		return false
	}
}

func (b *fileBrowser) move(delta int) {
	if len(b.entries) == 0 {
		return
	}
	b.cursor = (b.cursor + delta + len(b.entries)) % len(b.entries)
}

func (b *fileBrowser) selected() (browserEntry, bool) {
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return browserEntry{}, false
	}
	return b.entries[b.cursor], true
}

func (b *fileBrowser) enter() (string, bool, error) {
	entry, ok := b.selected()
	if !ok {
		return "", false, nil
	}
	if entry.dir {
		b.directory = entry.path
		b.cursor = 0
		if err := b.reload(); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	if b.purpose == browseSaveDatabase || b.purpose == browseSaveTemplate || b.purpose == browseSavePlayers || b.purpose == browseEditorSaveTemplate {
		return "", false, fmt.Errorf("choose a new filename with N; existing files are never overwritten")
	}
	if !entry.selectable {
		return "", false, fmt.Errorf("%s is not a supported file for this action", entry.name)
	}
	return entry.path, true, nil
}

func (b *fileBrowser) newPath(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
		return "", fmt.Errorf("enter a filename without directory components")
	}
	if b.purpose == browseSaveDatabase && filepath.Ext(filename) == "" {
		filename += ".sqlite"
	}
	if (b.purpose == browseSaveTemplate || b.purpose == browseSavePlayers || b.purpose == browseEditorSaveTemplate) && filepath.Ext(filename) == "" {
		filename += ".yaml"
	}
	if (b.purpose == browseSaveTemplate || b.purpose == browseSavePlayers || b.purpose == browseEditorSaveTemplate) && !b.matches(filename) {
		return "", fmt.Errorf("filename must end in .yaml or .yml")
	}
	if b.purpose == browseSaveDatabase && !strings.EqualFold(filepath.Ext(filename), ".db") && !strings.EqualFold(filepath.Ext(filename), ".sqlite") && !strings.EqualFold(filepath.Ext(filename), ".sqlite3") {
		return "", fmt.Errorf("database filename must end in .db, .sqlite, or .sqlite3")
	}
	path := filepath.Join(b.directory, filename)
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%s already exists; choose another filename", filename)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check destination: %w", err)
	}
	return path, nil
}
