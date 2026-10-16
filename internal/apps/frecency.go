package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Frecency (feat/logis story 7): how often and how recently each
// application was launched, persisted in
// $XDG_STATE_HOME/logis/frecency.json (default ~/.local/state) and turned
// into a boost for its match score.
//
// Each launch adds 1 to an application's rank and stamps the time. The
// boost weighs the rank by how long ago the last launch was (×4 within an
// hour, ×2 within a day, ×½ within a week, ×¼ after that) and grows with its
// logarithm, at most MaxBoost, so a much better match still wins over a
// favourite. When the ranks add up to more than MaxTotal, all of them age
// by a tenth and the faded ones are forgotten.
//
// The file is replaced atomically (written beside it, synced, renamed), so
// a crash leaves the old or the new one, never half of either. Its format
// is {"version":1,"apps":{ID:{"rank":…,"last":…}}}.

const (
	// MaxBoost is the largest boost frecency adds to a match score.
	MaxBoost = 48
	// MaxTotal is the ranks' sum above which they all age.
	MaxTotal = 1000.0
	// frecencyVersion is the file format's version.
	frecencyVersion = 1
)

// Frecency is the launches of every application, by desktop-file ID. It
// belongs to one goroutine; Clone hands a copy to another.
type Frecency struct{ apps map[string]use }

type use struct {
	Rank float64 `json:"rank"`
	// Last is seconds since the Unix epoch.
	Last uint64 `json:"last"`
}

type frecencyFile struct {
	Version uint32         `json:"version"`
	Apps    map[string]use `json:"apps"`
}

// NewFrecency is no history yet.
func NewFrecency() *Frecency { return &Frecency{apps: map[string]use{}} }

// FrecencyPath is where the frecency file lives in an environment; "" if
// nowhere (no absolute XDG_STATE_HOME or HOME).
func FrecencyPath(get func(string) string) string {
	state := get("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home := get("HOME")
		if !filepath.IsAbs(home) {
			return ""
		}
		state = filepath.Join(home, ".local/state")
	}
	return filepath.Join(state, "logis/frecency.json")
}

// LoadFrecency reads the file; a missing one is no history yet.
func LoadFrecency(path string) (*Frecency, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewFrecency(), nil
	}
	var doc frecencyFile
	if err == nil {
		err = json.Unmarshal(b, &doc)
	}
	if err != nil {
		return nil, fmt.Errorf("%s is unreadable: %w", path, err)
	}
	f := NewFrecency()
	for id, u := range doc.Apps {
		f.apps[id] = u
	}
	return f, nil
}

// Save writes the file atomically, creating its directory.
func (f *Frecency) Save(path string) error {
	b, err := json.Marshal(frecencyFile{Version: frecencyVersion, Apps: f.apps})
	if err != nil {
		return err
	}
	return writeAtomically(path, b)
}

// Clone is a copy of the history.
func (f *Frecency) Clone() *Frecency {
	c := NewFrecency()
	for id, u := range f.apps {
		c.apps[id] = u
	}
	return c
}

// Record notes a launch of id at now (seconds since the epoch).
func (f *Frecency) Record(id string, now uint64) {
	u, ok := f.apps[id]
	if !ok {
		u.Last = now
	}
	u.Rank++
	u.Last = max(u.Last, now)
	f.apps[id] = u
	total := 0.0
	for _, u := range f.apps {
		total += u.Rank
	}
	if total <= MaxTotal {
		return
	}
	for id, u := range f.apps {
		if u.Rank *= 0.9; u.Rank < 1 {
			delete(f.apps, id)
		} else {
			f.apps[id] = u
		}
	}
}

// Boost is how much id's match score is raised at now.
func (f *Frecency) Boost(id string, now uint64) uint32 {
	u, ok := f.apps[id]
	if !ok {
		return 0
	}
	var age uint64
	if now > u.Last {
		age = now - u.Last
	}
	weight := 0.25
	switch {
	case age < hour:
		weight = 4
	case age < day:
		weight = 2
	case age < 7*day:
		weight = 0.5
	}
	boost := math.Round(8 * math.Log1p(u.Rank*weight))
	return uint32(min(max(boost, 1), MaxBoost))
}

const (
	hour = 3600
	day  = 24 * hour
)

// Now is seconds since the Unix epoch, now.
func Now() uint64 { return uint64(max(time.Now().Unix(), 0)) }

// writeAtomically replaces path with b: a temporary file beside it, synced,
// then renamed over it.
func writeAtomically(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	err := writeSynced(tmp, b)
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func writeSynced(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
