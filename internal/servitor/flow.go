package servitor

import (
	"io"
	"slices"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// servitor mode as a provider flow (feat/logis stories 8–10, §4.8; Rust
// src/servitor/flow.rs): the census's rites, then the chosen rite's
// aspects, then one prompt per inscription of its canon, then `servitor
// invoke` and close. A pure state machine: what the census tells and what
// logis answers go in, the lines to say and the invocation to launch come
// out (run.go does the I/O). logis shows no outcome: servitor's vox
// announces the verdict.
//
// Nothing unsettled drives anything (story 10): a rite whose auspex is
// still awaited is pending (never chosen), and an aspect is marked current
// and preselected only from a settled entry (Entry.Current).

// Act is what the flow asks of its provider process: Say, Invoke or Exit.
type Act interface{ act() }

// Say is a line to logis.
type Say struct{ Message provider.Message }

// Invoke launches `servitor Args...`, detached; then the flow is told how
// that went (Flow.Launched).
type Invoke struct{ Args []string }

// Exit: the provider is done, with this exit status.
type Exit struct{ Code int }

func (Say) act()    {}
func (Invoke) act() {}
func (Exit) act()   {}

// where is where the flow stands.
type where int

const (
	atRites where = iota
	atAspect
	// atInscription: asking inscription len(words) of the rite's canon.
	atInscription
)

// Flow is one servitor-mode session. Log takes what is only logged (nil:
// nothing is).
type Flow struct {
	Log io.Writer
	// entries are every rite told, by name.
	entries   map[string]Entry
	concluded bool
	at        where
	rite      string
	// aspect is the chosen aspect; edict: there is none to choose.
	aspect string
	edict  bool
	// words are the inscriptions answered so far, in canon order.
	words []string
}

// Begin is the greeting and the rite stage, at once (the census fills it).
func (f *Flow) Begin() []Act {
	return []Act{Say{provider.Hello{Protocol: provider.Protocol}}, Say{ritesStage()}}
}

// Told: the census spoke, or ended.
func (f *Flow) Told(t Told) []Act {
	if t.Ended {
		switch {
		case f.concluded && len(f.entries) > 0:
			return nil
		case f.concluded:
			return []Act{toast(list.Info, noRites)}
		}
		return []Act{toast(list.Error, brokenOff(t.Status))}
	}
	switch h := t.Herald.(type) {
	case Rite:
		return f.riteTold(h.Entry)
	case Lament:
		about := ""
		if h.Rite != "" {
			about = " (" + h.Rite + ")"
		}
		f.log("the census laments%s: %s", about, h.Lament)
	case Concluded:
		f.concluded = true
	}
	return nil
}

// Unheard: the census could not be started.
func (f *Flow) Unheard(err error) []Act {
	return []Act{toast(list.Error, noServitor(err.Error()))}
}

// Heard: logis spoke.
func (f *Flow) Heard(m provider.ToProvider) []Act {
	switch m := m.(type) {
	case provider.Hello:
		if m.Protocol == provider.Protocol {
			return nil
		}
		f.log("logis speaks protocol %d, not %d", m.Protocol, provider.Protocol)
		return []Act{Exit{2}}
	case provider.Cancel:
		return []Act{Exit{0}}
	case provider.Activate:
		return f.activated(m.Stage, m.ID)
	case provider.Submit:
		return f.submitted(m.Stage, m.Text)
	}
	return nil
}

// Launched: the invocation was launched (err nil), or could not be.
func (f *Flow) Launched(err error) []Act {
	if err == nil {
		return []Act{Say{provider.Close{}}, Exit{0}}
	}
	return []Act{toast(list.Error, notRoused(err.Error())), ready()}
}

func (f *Flow) riteTold(e Entry) []Act {
	var acts []Act
	switch {
	case f.at == atRites:
		acts = []Act{Say{provider.Items{Items: []provider.Item{riteItem(&e)}}}}
	case f.at == atAspect && f.rite == e.Rite:
		items, sel := aspectItems(&e)
		acts = []Act{Say{provider.Items{Items: items, Select: sel}}}
	}
	if f.entries == nil {
		f.entries = map[string]Entry{}
	}
	f.entries[e.Rite] = e
	return acts
}

func (f *Flow) activated(stage, id string) []Act {
	switch {
	case f.at == atRites && stage == riteStage:
		return f.riteChosen(id)
	case f.at == atAspect && stage == aspectStage:
		if e, ok := f.entries[f.rite]; ok && slices.Contains(e.Aspects, id) {
			return f.inscribe(f.rite, id, false, nil)
		}
	}
	return []Act{ready()}
}

func (f *Flow) riteChosen(name string) []Act {
	e, ok := f.entries[name]
	if !ok || e.Awaited() {
		return []Act{ready()}
	}
	if e.Heretical() || (!e.Edict && len(e.Aspects) == 0) {
		return []Act{toast(list.Error, heretical(name)), ready()}
	}
	if e.Edict {
		return f.inscribe(name, "", true, nil)
	}
	items, sel := aspectItems(&e)
	f.at, f.rite = atAspect, name
	return []Act{Say{aspectStageOf(name, items, sel)}}
}

func (f *Flow) submitted(stage, text string) []Act {
	if f.at != atInscription {
		return []Act{ready()}
	}
	canon := f.canon(f.rite)
	if len(f.words) >= len(canon) {
		return []Act{ready()}
	}
	asked := &canon[len(f.words)]
	if stage != inscriptionID(asked) {
		return []Act{ready()}
	}
	if asked.Mandatory && text == "" {
		return []Act{toast(list.Error, mandatory(asked.Key)), ready()}
	}
	words := append(append([]string{}, f.words...), text)
	return f.inscribe(f.rite, f.aspect, f.edict, words)
}

// inscribe asks the next inscription, or invokes once every one is
// answered. The flow stays at the last question until the launch
// succeeds, so a failed launch can be answered again.
func (f *Flow) inscribe(rite, aspect string, edict bool, words []string) []Act {
	canon := f.canon(rite)
	if len(words) < len(canon) {
		f.at, f.rite, f.aspect, f.edict, f.words = atInscription, rite, aspect, edict, words
		return []Act{Say{inscriptionStage(&canon[len(words)], aspect, edict)}}
	}
	return []Act{Invoke{invocation(rite, aspect, edict, canon, words)}}
}

func (f *Flow) canon(rite string) []Declaration { return f.entries[rite].Canon }

func (f *Flow) log(format string, args ...any) {
	if f.Log != nil {
		logf(f.Log, format, args...)
	}
}

// invocation is `invoke <rite> [<aspect>] [--<key> <word>]...`: every
// word given, and an empty one where it clears a decree; an empty word
// without a decree is left out (it would inscribe nothing all the same).
func invocation(rite, aspect string, edict bool, canon []Declaration, words []string) []string {
	argv := []string{"invoke", rite}
	if !edict {
		argv = append(argv, aspect)
	}
	for i, word := range words {
		if _, decreed := canon[i].Decreed(aspect, edict); word != "" || decreed {
			argv = append(argv, "--"+canon[i].Key, word)
		}
	}
	return argv
}

func ready() Act { return Say{provider.Ready{}} }

func toast(level list.Level, text string) Act {
	seconds := toastSeconds
	return Say{provider.Toast{Text: text, Level: level, Seconds: &seconds}}
}
