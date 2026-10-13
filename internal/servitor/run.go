// Package servitor is servitor mode (feat/logis stories 8–10, §4.8; Rust
// src/servitor): a provider built into logis. `logis --servitor` opens a
// provider session whose provider is logis itself, `logis provide
// servitor`: the daemon runs it as the caller would have (the caller's
// working directory and environment, a process group of its own) and
// speaks the provider protocol with it (docs/provider-protocol.md). This
// package is that provider process. It keeps vigil over the census
// (`servitor census --vigil`, vigil.go), leads the user through the Flow,
// launches `servitor invoke …` detached (invoke.go) and closes. It uses
// servitor only through its CLI, found on the caller's PATH.
package servitor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nerdwave-nick/logis/internal/provider"
)

// Program is the program servitor mode speaks to.
const Program = "servitor"

// event is a line from logis (message), the end of logis's input
// (logisEnded), or what the census told.
type event struct {
	message    provider.ToProvider
	logisEnded bool
	told       *Told
}

// Run runs the provider on stdin and stdout until its flow is over and
// returns its exit status; program is servitor's CLI, logs go to stderr.
func Run(program string, stdin io.Reader, stdout, stderr io.Writer) int {
	events := make(chan event)
	done := make(chan struct{})
	defer close(done)
	send := func(e event) bool {
		select {
		case events <- e:
			return true
		case <-done:
			return false
		}
	}
	go listenToLogis(stdin, stderr, send)
	flow := &Flow{Log: stderr}
	acts := flow.Begin()
	censusStarted := false
	for {
		for len(acts) > 0 {
			act := acts[0]
			acts = acts[1:]
			switch a := act.(type) {
			case Say:
				if _, err := stdout.Write(provider.Say(a.Message)); err != nil {
					return 0 // logis is gone: nobody is left to answer
				}
			case Invoke:
				acts = append(acts, flow.Launched(Detached(program, a.Args))...)
			case Exit:
				return a.Code
			}
		}
		// The census starts once the first stage is out, so the window
		// shows at once however the census fares.
		if !censusStarted {
			censusStarted = true
			err := KeepVigil(program, stderr, func(t Told) bool { return send(event{told: &t}) })
			if err != nil {
				acts = flow.Unheard(err)
			}
			continue
		}
		e := <-events
		switch {
		case e.logisEnded:
			return 0
		case e.told != nil:
			acts = flow.Told(*e.told)
		default:
			acts = flow.Heard(e.message)
		}
	}
}

// listenToLogis hears logis's lines on stdin; lines of a type this version
// does not know are passed over, as the protocol asks.
func listenToLogis(stdin io.Reader, stderr io.Writer, send func(event) bool) {
	lines := bufio.NewReader(stdin)
	for {
		line, err := lines.ReadBytes('\n')
		m, herr := heard(bytes.TrimSuffix(line, []byte("\n")))
		switch {
		case herr != nil:
			logf(stderr, "%v", herr)
		case m != nil && !send(event{message: m}):
			return
		}
		if err != nil {
			send(event{logisEnded: true})
			return
		}
	}
}

// heard reads one line from logis: a message, nil for a blank line or a
// type yet unknown, or an error.
func heard(line []byte) (provider.ToProvider, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, nil
	}
	var tagged struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal(line, &tagged); err != nil {
		return nil, fmt.Errorf("logis spoke no JSON line: %v", err)
	}
	kind, _ := tagged.Type.(string)
	decode, known := fromLogis[kind]
	if !known {
		return nil, nil
	}
	m, err := decode(line)
	if err != nil {
		return nil, fmt.Errorf("a malformed '%s' from logis: %v", kind, err)
	}
	return m, nil
}

// fromLogis read each known type; every field is required, as serde has it.
var fromLogis = map[string]func([]byte) (provider.ToProvider, error){
	"hello": func(b []byte) (provider.ToProvider, error) {
		var w struct {
			Protocol *uint32 `json:"protocol"`
		}
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		if w.Protocol == nil {
			return nil, errors.New("missing field `protocol`")
		}
		return provider.Hello{Protocol: *w.Protocol}, nil
	},
	"activate": func(b []byte) (provider.ToProvider, error) {
		stage, id, err := twoStrings(b, "stage", "id")
		return provider.Activate{Stage: stage, ID: id}, err
	},
	"submit": func(b []byte) (provider.ToProvider, error) {
		stage, text, err := twoStrings(b, "stage", "text")
		return provider.Submit{Stage: stage, Text: text}, err
	},
	"cancel": func([]byte) (provider.ToProvider, error) { return provider.Cancel{}, nil },
}

// twoStrings reads the required string fields a and b.
func twoStrings(line []byte, a, b string) (string, string, error) {
	var w map[string]json.RawMessage
	if err := json.Unmarshal(line, &w); err != nil {
		return "", "", err
	}
	var out [2]string
	for i, name := range []string{a, b} {
		raw, ok := w[name]
		if !ok {
			return "", "", fmt.Errorf("missing field `%s`", name)
		}
		if err := json.Unmarshal(raw, &out[i]); err != nil || bytes.Equal(raw, []byte("null")) {
			return "", "", fmt.Errorf("invalid type for `%s`: %s, expected a string", name, raw)
		}
	}
	return out[0], out[1], nil
}

// logf writes one line to the log (the daemon's, through stderr).
func logf(stderr io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(stderr, "logis servitor: "+format+"\n", args...)
}
