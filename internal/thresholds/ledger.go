// Package thresholds remembers, per quota meter, the last alert step that
// was already sent — so a process restart doesn't re-announce something it
// already told you about, and a meter resetting (e.g. a weekly window
// rolling over) is visible as a drop rather than silence.
package thresholds

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Ledger is a flat map from a meter's identifier to the highest step
// percentage already announced for it.
type Ledger struct {
	Announced map[string]int `json:"announced"`
}

func empty() *Ledger {
	return &Ledger{Announced: map[string]int{}}
}

// Open reads a ledger from disk. A missing file just means nothing has been
// announced yet, so it returns an empty ledger rather than an error.
func Open(path string) (*Ledger, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty(), nil
	}
	if err != nil {
		return nil, err
	}

	l := empty()
	if err := json.Unmarshal(raw, l); err != nil {
		return nil, err
	}
	if l.Announced == nil {
		l.Announced = map[string]int{}
	}
	return l, nil
}

// Persist writes the ledger to disk, creating any missing parent directory.
func (l *Ledger) Persist(path string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Advance records newStep for id and reports whether that's higher than
// what was previously announced (i.e. whether this is worth telling someone
// about). A drop — newStep below what's on record — just resets the record
// without being reported as noteworthy.
func (l *Ledger) Advance(id string, newStep int) (noteworthy bool) {
	prev := l.Announced[id]
	l.Announced[id] = newStep
	return newStep > prev
}
