package main

import (
	"bufio"
	"io"
	"strconv"

	"github.com/nerdwave-nick/logis/internal/nth"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// The client side of a dmenu session (feat/logis stories 1–4, §4.6; Rust
// src/client/dmenu): stdin streams to the daemon as items while the session
// is open (the window opens before the input ends), and the answer comes
// back on stdout: the chosen item's original bytes (or its --accept-nth
// columns, or its --index), or the typed text when it matched nothing.
// Exit 0 with a choice, 1 with empty stdout when dismissed, 2 on error.

// exitDismissed is the exit status of a session that ended without a
// choice.
const exitDismissed = 1

const (
	// batchItems is the most items one frame carries,
	batchItems = 1024
	// batchBytes about the most bytes of item text.
	batchBytes = 256 << 10
)

// runDmenu runs a dmenu session; configPath goes to a daemon this client
// has to start.
func runDmenu(configPath string, d dmenuOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	var input *bufio.Reader
	if d.readStdin {
		input = bufio.NewReaderSize(stdin, 64<<10)
		if d.noRunIfEmpty {
			// Nothing to choose from: no window at all.
			if _, err := input.Peek(1); err != nil {
				return exitDismissed
			}
		}
	}
	outcome, err := dmenuSession(configPath, input, d, stderr)
	if err != nil {
		return fail(stderr, err.Error())
	}
	printed, ok := choice(outcome, d)
	if !ok {
		return exitDismissed
	}
	if _, err := stdout.Write(append(printed, '\n')); err != nil {
		return fail(stderr, "the choice could not be inscribed on stdout: "+err.Error())
	}
	return 0
}

// dmenuSession opens the session, streams input to it (or sends end at
// once without), and waits for its outcome.
func dmenuSession(configPath string, input *bufio.Reader, d dmenuOptions, stderr io.Writer) (protocol.Outcome, error) {
	conn, err := connect(configPath, stderr)
	if err != nil {
		return protocol.Outcome{}, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.Send(protocol.Open(protocol.ModeDmenu, protocol.EncodeOptions(d.view))); err != nil {
		return protocol.Outcome{}, err
	}
	if input == nil {
		if err := conn.Send(protocol.End()); err != nil {
			return protocol.Outcome{}, err
		}
	} else {
		// The daemon may answer before stdin ends; the process's exit ends
		// this goroutine.
		go streamItems(input, d.separator,
			func(raw []byte) protocol.Item { return dmenuItem(raw, d.columns) },
			func(r protocol.Request) bool { return conn.Send(r) == nil })
	}
	return conn.Await()
}

// choice is what an outcome prints: the item's index (--index), its
// --accept-nth columns or its original bytes; typed text as typed (also
// with --index, as fuzzel documents free text: "printed as is"). False
// when nothing was chosen.
func choice(o protocol.Outcome, d dmenuOptions) ([]byte, bool) {
	switch {
	case o.Kind == protocol.Selected && d.index:
		return []byte(strconv.FormatUint(o.Index, 10)), true
	case o.Kind == protocol.Selected:
		return d.columns.Accepted(o.Item.Raw), true
	case o.Kind == protocol.Typed:
		return []byte(o.Text), true
	}
	return nil, false
}

// dmenuItem is one input line as an item: shown as its --with-nth columns
// (or whole), matched by its --match-nth columns (or as shown), its bytes
// kept as read.
func dmenuItem(raw []byte, c nth.Columns) protocol.Item {
	item := protocol.Item{Text: c.Shown(raw), Raw: raw}
	if match, ok := c.Matched(raw); ok {
		item.Match = &match
	}
	return item
}

// streamItems reads lines ended by separator and sends them as items
// frames, then end. A batch goes out whenever the input has nothing more
// buffered, so items reach the daemon as they arrive. It stops early when
// send fails.
func streamItems(r *bufio.Reader, separator byte, item func([]byte) protocol.Item, send func(protocol.Request) bool) {
	var batch []protocol.Item
	bytes := 0
	for {
		line, err := r.ReadBytes(separator)
		if len(line) == 0 && err != nil {
			break
		}
		if line[len(line)-1] == separator {
			line = line[:len(line)-1]
		}
		bytes += len(line)
		batch = append(batch, item(line))
		if len(batch) >= batchItems || bytes >= batchBytes || r.Buffered() == 0 {
			if !send(protocol.Items(batch)) {
				return
			}
			batch, bytes = nil, 0
		}
		if err != nil {
			break
		}
	}
	if len(batch) > 0 && !send(protocol.Items(batch)) {
		return
	}
	send(protocol.End())
}
