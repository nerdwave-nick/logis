package servitor

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Reading servitor's census, herald by herald.

func TestARecordedVigilReadsRiteByRiteThenItsConclusion(t *testing.T) {
	heralds := recorded(t, "vigil.jsonl")
	var names []string
	for _, h := range heralds[:7] {
		names = append(names, rite(t, h).Rite)
	}
	if want := []string{"broken", "dnd", "hush", "screenshot", "theme", "hush", "dnd"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("%q", names)
	}
	if _, ok := heralds[7].(Concluded); !ok || len(heralds) != 8 {
		t.Fatalf("%#v", heralds[7:])
	}
	dnd := rite(t, heralds[1])
	if !dnd.Awaited() {
		t.Fatal("dnd is awaited")
	}
	if a, ok := dnd.Current(); ok {
		t.Fatalf("current %q while awaited", a)
	}
	if !reflect.DeepEqual(dnd.Aspects, []string{"on", "off"}) {
		t.Fatalf("%q", dnd.Aspects)
	}
	settled := rite(t, heralds[6])
	if a, ok := settled.Current(); settled.Auspex != "heeded" || !ok || a != "on" {
		t.Fatalf("%q %q %v", settled.Auspex, a, ok)
	}
	reason := settled.Canon[0]
	if reason.Key != "reason" || !reason.Mandatory || reason.Purpose != "why the silence was imposed" {
		t.Fatalf("%+v", reason)
	}
	origin := settled.Canon[1]
	if w, ok := origin.Decreed("off", false); !ok || w != "the cogitator" {
		t.Fatalf("%q %v", w, ok)
	}
	if w, ok := origin.Decreed("on", false); !ok || w != "the hotkey" {
		t.Fatalf("%q %v", w, ok)
	}
	hush := rite(t, heralds[5])
	if hush.Awaited() {
		t.Fatal("hush settles mute")
	}
	if a, ok := hush.Current(); ok {
		t.Fatalf("hush stands in no aspect, not %q", a)
	}
}

func TestEdictsAndHereticalRitesReadAsServitorTellsThem(t *testing.T) {
	heralds := recorded(t, "vigil.jsonl")
	broken := rite(t, heralds[0])
	if !broken.Heretical() || len(broken.Canon) != 0 || len(broken.Aspects) != 0 {
		t.Fatalf("%+v", broken)
	}
	edict := rite(t, heralds[3])
	if !edict.Edict || len(edict.Aspects) != 0 {
		t.Fatalf("%+v", edict)
	}
	if a, ok := edict.Current(); ok {
		t.Fatalf("an edict stands in %q", a)
	}
	if w, ok := edict.Canon[0].Decreed("", true); !ok || w != "chosen" {
		t.Fatalf("%q %v", w, ok)
	}
	if len(edict.Canon[0].Decrees) != 0 {
		t.Fatalf("%v", edict.Canon[0].Decrees)
	}
}

func TestLamentsAndUnknownHeraldsAndKeysAreReadOrPassedOver(t *testing.T) {
	lament := mustParse(t, `{"herald":"lament","lament":"the data-slate is garbled","rite":"theme"}`)
	if want := (Lament{Lament: "the data-slate is garbled", Rite: "theme"}); lament != want {
		t.Fatalf("%#v", lament)
	}
	if h := mustParse(t, `{"herald":"omen","x":1}`); h != (UnknownHerald{}) {
		t.Fatalf("%#v", h)
	}
	later := rite(t, mustParse(t, `{"herald":"rite","rite":"t","auspex":"","aspects":["a"],"aspect":"a","sigil":7}`))
	if a, ok := later.Current(); !ok || a != "a" {
		t.Fatalf("%q %v", a, ok)
	}
	for _, bad := range []string{`not json`, `{"rite":"t"}`, `{"herald":"lament"}`, `{"herald":"rite","edict":"yes"}`} {
		if _, err := ParseHerald([]byte(bad)); err == nil || !strings.Contains(err.Error(), "a census line that cannot be read") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestOnlyASettledAspectOfTheRiteIsCurrent(t *testing.T) {
	for _, c := range []struct {
		auspex, aspect, current string
		ok                      bool
	}{
		{"", "on", "on", true},
		{"heeded", "off", "off", true},
		{"mute", "on", "on", true},
		{"awaited", "on", "", false},
		{"forgone", "on", "", false},
		{"a marker of a later servitor", "on", "", false},
		{"heeded", "", "", false},
		{"heeded", "sideways", "", false},
	} {
		e := Entry{Auspex: c.auspex, Aspect: c.aspect, Aspects: []string{"on", "off"}}
		if a, ok := e.Current(); a != c.current || ok != c.ok {
			t.Errorf("%s %s: %q %v", c.auspex, c.aspect, a, ok)
		}
	}
}

func TestTheVigilIsHeardLineByLineThenItsEnd(t *testing.T) {
	dir := t.TempDir()
	told := make(chan Told, 64)
	program := fake(t, dir, fixture(t, "vigil.jsonl"))
	if _, err := KeepVigil(program, io.Discard, func(tl Told) bool { told <- tl; return true }); err != nil {
		t.Fatal(err)
	}
	var heralds []Herald
	for {
		select {
		case tl := <-told:
			if !tl.Ended {
				heralds = append(heralds, tl.Herald)
				continue
			}
			if tl.Status != "exit status: 0" {
				t.Fatalf("%q", tl.Status)
			}
			if !reflect.DeepEqual(heralds, recorded(t, "vigil.jsonl")) {
				t.Fatalf("%#v", heralds)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("the vigil never ended")
		}
	}
}

func TestACensusThatCannotStartIsAnError(t *testing.T) {
	_, err := KeepVigil("/nonexistent/servitor", io.Discard, func(Told) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("%v", err)
	}
}

// A reader that wants no more closes the census's stdout, and the census,
// which keeps writing, ends.
func TestAReaderThatDepartsEndsTheCensus(t *testing.T) {
	dir := t.TempDir()
	program := fake(t, dir, fixture(t, "lingering.jsonl"))
	heard := make(chan struct{}, 1)
	if _, err := KeepVigil(program, io.Discard, func(Told) bool { heard <- struct{}{}; return false }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Fatal("the census told nothing")
	}
	pid := censusPid(t, dir)
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("the census outlived its reader")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
