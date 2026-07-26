// Ownership summary:
//   This file implements domain logic for the javflow backend.
//
// File map for maintainers:
//   1) Domain-specific types and helpers.
//   2) Internal service implementation.
//

package organizer

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type renameHistoryEntry struct {
	OriginalName string `json:"originalName"`
	OriginalPath string `json:"originalPath"`
	WaitingPath  string `json:"waitingPath"`
	NewName      string `json:"newName"`
	FilmCode     string `json:"filmCode"`
	RecordedAt   string `json:"recordedAt"`
}

func appendRenameHistory(historyPath string, records []RenameRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := ensureDirectory(filepath.Dir(historyPath)); err != nil {
		return err
	}
	file, err := os.OpenFile(historyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, record := range records {
		if strings.TrimSpace(record.WaitingPath) == "" {
			continue
		}
		entry := renameHistoryEntry{
			OriginalName: record.OriginalName,
			OriginalPath: record.OriginalPath,
			WaitingPath:  record.WaitingPath,
			NewName:      record.NewName,
			FilmCode:     record.FilmCode,
			RecordedAt:   time.Now().Format(time.RFC3339),
		}
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}

func loadRenameHistoryHints(historyPath string) (map[string]string, map[string]string) {
	pathHints := map[string]string{}
	nameHints := map[string]string{}
	file, err := os.Open(historyPath)
	if err != nil {
		return pathHints, nameHints
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := renameHistoryEntry{}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		hint := strings.TrimSpace(entry.OriginalName + " " + entry.OriginalPath)
		if hint == "" {
			continue
		}
		if waitingPath := strings.ToLower(strings.TrimSpace(entry.WaitingPath)); waitingPath != "" {
			pathHints[waitingPath] = hint
		}
		if newName := strings.ToLower(strings.TrimSpace(entry.NewName)); newName != "" {
			if existing, ok := nameHints[newName]; ok && existing != hint {
				nameHints[newName] = ""
			} else {
				nameHints[newName] = hint
			}
		}
	}
	return pathHints, nameHints
}
