package servitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// servitor's streaming census, `servitor census --vigil` (servitor caller
// interface S1, S4, S5; codex passages vigil, herald, canon): one JSON
// object per line, tagged by "herald". A
// rite line is a whole census entry that replaces any earlier one of the
// same rite; lament lines may come anywhere; concluded comes once, last.
// Heralds and keys only ever grow, so unknown ones are passed over.

// Herald is one line of the vigil: Rite, Lament, Concluded, or
// UnknownHerald (a herald of a later servitor).
type Herald interface{ herald() }

// Rite is a rite, whole.
type Rite struct{ Entry }

// Lament is a lament, such as a data-slate that cannot be read; Rite names
// the rite it concerns ("" when none).
type Lament struct {
	Lament string
	Rite   string
}

// Concluded: every rite is settled; the last line.
type Concluded struct{}

// UnknownHerald is a herald of a later servitor.
type UnknownHerald struct{}

func (Rite) herald()          {}
func (Lament) herald()        {}
func (Concluded) herald()     {}
func (UnknownHerald) herald() {}

// Entry is a census entry, as far as logis reads it (last_rite and
// recorded_in are not needed and are passed over like any key yet unknown).
type Entry struct {
	Rite  string `json:"rite"`
	Edict bool   `json:"edict"`
	// Aspect is the aspect it stands in; "" when none is known (and for
	// an edict).
	Aspect string `json:"aspect"`
	// Standing: performed, dormant, corrupted, tainted, desecrated,
	// heretical, proclaimed.
	Standing string `json:"standing"`
	// Auspex is what its auspex gave: "", forgone, awaited, heeded or mute.
	Auspex string `json:"auspex"`
	// Aspects in written order; empty for an edict.
	Aspects    []string `json:"aspects"`
	Purpose    string   `json:"purpose"`
	Desecrated bool     `json:"desecrated"`
	// Canon is its inscriptions as declared, in written order.
	Canon []Declaration `json:"canon"`
}

// Declaration is one inscription of a rite's canon (S5): every field is
// always present.
type Declaration struct {
	Key       string `json:"key"`
	Purpose   string `json:"purpose"`
	Mandatory bool   `json:"mandatory"`
	// Decrees is the decreed word per aspect, "*" already resolved;
	// aspects without a decree are absent. Empty for an edict.
	Decrees map[string]string `json:"decrees"`
	// Decree is an edict's one decree; nil for a rite.
	Decree *string `json:"decree"`
}

// Awaited: its auspex has not settled yet; the entry will be told again.
func (e *Entry) Awaited() bool { return e.Auspex == "awaited" }

// Current is the aspect it stands in, when it may be trusted: the entry is
// settled by an auspex that answered, failed or does not exist (never
// awaited, nor forgone or a marker of a later servitor), and the aspect is
// one of its own.
func (e *Entry) Current() (string, bool) {
	settled := e.Auspex == "" || e.Auspex == "heeded" || e.Auspex == "mute"
	if settled && !e.Edict && slices.Contains(e.Aspects, e.Aspect) {
		return e.Aspect, true
	}
	return "", false
}

// Heretical: the rite stands in heresy.
func (e *Entry) Heretical() bool { return e.Standing == "heretical" }

// Decreed is the word servitor inscribes for aspect (or, for an edict, ""
// with edict true) when the invocation gives none.
func (d *Declaration) Decreed(aspect string, edict bool) (string, bool) {
	if edict {
		if d.Decree == nil {
			return "", false
		}
		return *d.Decree, true
	}
	word, ok := d.Decrees[aspect]
	return word, ok
}

// ParseHerald reads one line of the vigil.
func ParseHerald(line []byte) (Herald, error) {
	h, err := parseHerald(line)
	if err != nil {
		return nil, fmt.Errorf("a census line that cannot be read: %v", err)
	}
	return h, nil
}

func parseHerald(line []byte) (Herald, error) {
	var tagged struct {
		Herald *string `json:"herald"`
	}
	if err := json.Unmarshal(line, &tagged); err != nil {
		return nil, err
	}
	if tagged.Herald == nil {
		return nil, errors.New("missing field `herald`")
	}
	switch *tagged.Herald {
	case "rite":
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, err
		}
		return Rite{e}, nil
	case "lament":
		var l struct {
			Lament *string `json:"lament"`
			Rite   *string `json:"rite"`
		}
		if err := json.Unmarshal(line, &l); err != nil {
			return nil, err
		}
		if l.Lament == nil {
			return nil, errors.New("missing field `lament`")
		}
		out := Lament{Lament: *l.Lament}
		if l.Rite != nil {
			out.Rite = *l.Rite
		}
		return out, nil
	case "concluded":
		return Concluded{}, nil
	}
	return UnknownHerald{}, nil
}
