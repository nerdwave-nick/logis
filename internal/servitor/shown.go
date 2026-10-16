package servitor

import (
	"github.com/nerdwave-nick/logis/internal/provider"
)

// How servitor mode looks: its stages, rows and words (feat/logis §4.8; the
// mapping in docs/provider-protocol.md, "servitor mode on this protocol").

// The stage ids. An inscription's is "inscription:<key>".
const (
	riteStage   = "rite"
	aspectStage = "aspect"
)

// toastSeconds is how long a notice of trouble shows.
const toastSeconds uint32 = 6

const (
	// unknown is what "·" stands for, as in servitor's own census table:
	// not known.
	unknown = "·"
	// The emblems of a rite and of an edict.
	riteGlyph  = "⚙"
	edictGlyph = "†"
	// current is the badge of the aspect a rite stands in.
	current = "current"
)

const noRites = "the Librarium holds no rite"

// ritesStage is the rite stage, empty: the census fills it as it speaks.
func ritesStage() provider.Stage {
	prompt := "rite: "
	return provider.Stage{ID: riteStage, Prompt: &prompt, Placeholder: "choose a rite"}
}

// riteItem is a rite's row: its name, its aspect when known and its
// purpose. While its auspex is awaited it is pending, and nothing it says
// of its aspect or standing is shown yet.
func riteItem(e *Entry) provider.Item {
	pending := e.Awaited()
	aspect, ok := e.Current()
	if !ok {
		aspect = unknown
	}
	badges := []string{}
	if e.Edict {
		badges = append(badges, "edict")
	}
	if !pending {
		switch {
		case e.Standing == "heretical":
			badges = append(badges, "heresy")
		case e.Standing == "corrupted" || e.Standing == "tainted":
			badges = append(badges, e.Standing)
		case e.Desecrated:
			badges = append(badges, "desecrated")
		}
	}
	glyph := riteGlyph
	if e.Edict {
		glyph = edictGlyph
	}
	return provider.Item{
		ID:      e.Rite,
		Columns: []string{e.Rite, aspect, e.Purpose},
		Glyph:   &glyph,
		Badges:  badges,
		Pending: pending,
	}
}

// aspectItems are a rite's aspects in written order, the current one
// marked, and the one to select: the current aspect, only when it may be
// trusted.
func aspectItems(e *Entry) ([]provider.Item, *string) {
	cur, ok := e.Current()
	items := make([]provider.Item, 0, len(e.Aspects))
	for _, a := range e.Aspects {
		badges := []string{}
		if ok && a == cur {
			badges = append(badges, current)
		}
		items = append(items, provider.Item{ID: a, Columns: []string{a}, Badges: badges})
	}
	if !ok {
		return items, nil
	}
	return items, &cur
}

func aspectStageOf(rite string, items []provider.Item, sel *string) provider.Stage {
	prompt := "aspect: "
	return provider.Stage{
		ID:          aspectStage,
		Prompt:      &prompt,
		Placeholder: "bring " + rite + " into which aspect?",
		Items:       items,
		Select:      sel,
	}
}

func inscriptionID(d *Declaration) string { return "inscription:" + d.Key }

// inscriptionStage is the prompt for one inscription: its purpose, its
// decree for the aspect (or the edict's) as the query, and a word
// required when it is mandatory.
func inscriptionStage(d *Declaration, aspect string, edict bool) provider.Stage {
	prompt, input := d.Key+": ", provider.InputOptional
	if d.Mandatory {
		prompt, input = d.Key+"*: ", provider.InputRequired
	}
	placeholder := d.Purpose
	if placeholder == "" {
		placeholder = "the inscription " + d.Key
	}
	query, _ := d.Decreed(aspect, edict)
	return provider.Stage{
		ID:          inscriptionID(d),
		Prompt:      &prompt,
		Placeholder: placeholder,
		Query:       query,
		Input:       input,
	}
}

func heretical(rite string) string {
	return "the rite " + rite + " stands in heresy: the inquisition must purge it before it is invoked"
}

func mandatory(key string) string {
	return "the inscription " + key + " is mandatory: inscribe a word"
}

func noServitor(err string) string { return "no servitor answers the summons: " + err }

func brokenOff(status string) string {
	return "the census fell silent before its end (" + status + "); see the cogitator's log"
}

func notRoused(err string) string { return "the servitor could not be roused: " + err }
