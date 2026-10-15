package servitor

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/provider"
)

// Ported from Rust src/servitor/mod.rs tests.
func TestLogisLinesAreReadAndUnknownOnesPassedOver(t *testing.T) {
	m, err := heard([]byte(`{"type":"activate","stage":"rite","id":"dnd","later":1}`))
	if err != nil || m != (provider.Activate{Stage: "rite", ID: "dnd"}) {
		t.Fatalf("%#v, %v", m, err)
	}
	for line, want := range map[string]provider.ToProvider{
		`{"type":"cancel"}`:                       provider.Cancel{},
		`{"type":"hello","protocol":1}`:           provider.Hello{Protocol: 1},
		`{"type":"submit","stage":"s","text":""}`: provider.Submit{Stage: "s"},
		`{"type":"omen"}`:                         nil,
		`  `:                                      nil,
	} {
		if m, err := heard([]byte(line)); err != nil || m != want {
			t.Errorf("%s: %#v, %v", line, m, err)
		}
	}
	for _, bad := range []string{`garbage`, `{"type":"submit","stage":1}`, `{"type":"activate","stage":"rite"}`, `{"type":"hello"}`} {
		if _, err := heard([]byte(bad)); err == nil {
			t.Errorf("%s read", bad)
		}
	}
}

// provide runs Run in-process on pipes, servitor being program; what it
// says is read as it comes, as an OS pipe's buffer would take it. When the
// test ends, Run ends and so does the census it started, before the test's
// directories go: the fake census writes its records there.
type provide struct {
	t        *testing.T
	in       *io.PipeWriter
	lines    chan string
	exit     chan int
	returned chan *Vigil
}

func startProvide(t *testing.T, program string) *provide {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	p := &provide{t: t, in: inW, lines: make(chan string, 64), exit: make(chan int, 1), returned: make(chan *Vigil, 1)}
	go func() {
		code, vigil := run(program, inR, outW, io.Discard)
		_ = outW.Close()
		p.exit <- code
		p.returned <- vigil
	}()
	go func() {
		out := bufio.NewReader(outR)
		for {
			line, err := out.ReadString('\n')
			if err != nil {
				close(p.lines)
				return
			}
			p.lines <- line
		}
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
		select {
		case vigil := <-p.returned:
			if vigil != nil {
				select {
				case <-vigil.Ended():
				case <-time.After(5 * time.Second):
					t.Error("the census outlived its provider")
				}
			}
		case <-time.After(5 * time.Second):
			t.Error("the provider never ended")
		}
	})
	return p
}

func (p *provide) say(line string) {
	p.t.Helper()
	written := make(chan error, 1)
	go func() { _, err := io.WriteString(p.in, line+"\n"); written <- err }()
	select {
	case err := <-written:
		if err != nil {
			p.t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		p.t.Fatal("the provider does not hear logis")
	}
}

// next is the next line said, as JSON.
func (p *provide) next() map[string]any {
	p.t.Helper()
	var line string
	select {
	case l, ok := <-p.lines:
		if !ok {
			p.t.Fatal("the provider said no more")
		}
		line = l
	case <-time.After(5 * time.Second):
		p.t.Fatal("the provider said nothing")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		p.t.Fatal(err)
	}
	return v
}

func (p *provide) ended() int {
	p.t.Helper()
	select {
	case code := <-p.exit:
		return code
	case <-time.After(5 * time.Second):
		p.t.Fatal("the provider never ended")
		return -1
	}
}

func TestAnEdictRunsFromGreetingToItsDetachedInvocation(t *testing.T) {
	dir := t.TempDir()
	p := startProvide(t, fake(t, dir, fixture(t, "edict.jsonl")))
	p.say(`{"type":"hello","protocol":1}`)
	if m := p.next(); m["type"] != "hello" || m["protocol"] != 1.0 {
		t.Fatalf("%v", m)
	}
	if m := p.next(); m["type"] != "stage" || m["id"] != "rite" {
		t.Fatalf("%v", m)
	}
	m := p.next()
	if m["type"] != "items" || m["items"].([]any)[0].(map[string]any)["id"] != ">screenshot" {
		t.Fatalf("%v", m)
	}
	p.say(`{"type":"activate","stage":"rite","id":">screenshot"}`)
	if m := p.next(); m["type"] != "stage" || m["id"] != "inscription:region" || m["query"] != "chosen" {
		t.Fatalf("%v", m)
	}
	p.say(`{"type":"submit","stage":"inscription:region","text":"whole"}`)
	if m := p.next(); m["type"] != "close" {
		t.Fatalf("%v", m)
	}
	if code := p.ended(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !waitFor(filepath.Join(dir, "invoke", "recorded"), 5*time.Second) {
		t.Fatal("never invoked")
	}
	if argv := strings.Fields(record(t, dir, "argv")); !reflect.DeepEqual(argv, []string{">screenshot", "--region", "whole"}) {
		t.Fatalf("%q", argv)
	}
	// It labours on, unwatched, to its end (before its records go).
	if !waitFor(filepath.Join(dir, "invoke", "done"), 5*time.Second) {
		t.Fatal("never done")
	}
}

func TestCancelOrTheEndOfLogissInputEndsTheProvider(t *testing.T) {
	p := startProvide(t, fake(t, t.TempDir(), fixture(t, "lingering.jsonl")))
	p.next()
	p.say(`{"type":"cancel"}`)
	if code := p.ended(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	p = startProvide(t, fake(t, t.TempDir(), fixture(t, "lingering.jsonl")))
	p.next()
	_ = p.in.Close()
	if code := p.ended(); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestAForeignProtocolEndsTheProviderWithExit2(t *testing.T) {
	p := startProvide(t, fake(t, t.TempDir(), fixture(t, "lingering.jsonl")))
	p.say(`{"type":"hello","protocol":2}`)
	if code := p.ended(); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

// Run's return is the provider's exit, which ends the census (its stdout
// closes, S4): the census must not outlive Run in a caller's process, still
// running and writing its records after the caller cleaned up (on CI
// TestAForeignProtocolEndsTheProviderWithExit2 failed with "TempDir
// RemoveAll cleanup: … directory not empty"). Once its rite is told, a
// lingering census only sends heartbeats, which tell nothing, so only
// Run's own return can close its stdout.
func TestACensusDoesNotOutliveItsProvider(t *testing.T) {
	dir := t.TempDir()
	p := startProvide(t, fake(t, dir, fixture(t, "lingering.jsonl")))
	p.say(`{"type":"hello","protocol":1}`)
	for _, want := range []string{"hello", "stage", "items"} {
		if m := p.next(); m["type"] != want {
			t.Fatalf("%v, want %s", m, want)
		}
	}
	p.say(`{"type":"cancel"}`)
	if code := p.ended(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	pid := censusPid(t, dir)
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("the census outlived its provider")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNoServitorIsToldInTheWindow(t *testing.T) {
	p := startProvide(t, filepath.Join(t.TempDir(), "servitor"))
	p.next()
	p.next()
	if m := p.next(); m["type"] != "toast" || m["level"] != "error" ||
		!strings.HasPrefix(m["text"].(string), "no servitor answers the summons: ") {
		t.Fatalf("%v", m)
	}
	p.say(`{"type":"cancel"}`)
	p.ended()
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestAProviderWhoseLogisIsGoneEndsQuietly(t *testing.T) {
	if code := Run(fake(t, t.TempDir(), fixture(t, "lingering.jsonl")), strings.NewReader(""), failing{}, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
}
