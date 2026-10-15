package servitor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"

	"github.com/nerdwave-nick/logis/internal/provider"
)

// Told is what the vigil tells: a Herald, or, once its stdout closed, that
// the census Ended and how (Status, as Rust's ExitStatus says it).
type Told struct {
	Herald Herald
	Ended  bool
	Status string
}

// Vigil is a census being kept.
type Vigil struct {
	stdout io.Closer
	ended  chan struct{}
}

// Stop closes the census's stdout, as the provider's exit does: the census
// ends at its next line or heartbeat (S4). It does not wait for that.
func (v *Vigil) Stop() { _ = v.stdout.Close() }

// Ended is closed once the census has ended and been reaped.
func (v *Vigil) Ended() <-chan struct{} { return v.ended }

// KeepVigil starts `program census --vigil` with stdin on /dev/null and
// stderr on stderr (the daemon's log); told hears each line on a goroutine
// of its own while it returns true, then how the census ended. Lines that
// cannot be read are logged and passed over. Once told returns false, or
// the vigil is stopped, the census's stdout is closed, which ends the
// census and its auspexes (S4), as the provider's own exit does.
func KeepVigil(program string, stderr io.Writer, told func(Told) bool) (*Vigil, error) {
	cmd := exec.Command(program, "census", "--vigil")
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return nil, startError(err)
	}
	v := &Vigil{stdout: stdout, ended: make(chan struct{})}
	go func() {
		defer close(v.ended)
		if !hearVigil(stdout, stderr, told) {
			v.Stop()
			_ = cmd.Wait()
			return
		}
		err := cmd.Wait()
		status := "unknown (" + fmt.Sprint(err) + ")"
		if cmd.ProcessState != nil {
			status = provider.StatusOf(cmd.ProcessState)
		}
		told(Told{Ended: true, Status: status})
	}()
	return v, nil
}

// hearVigil tells each line until the census's stdout ends (true) or told
// wants no more (false).
func hearVigil(stdout io.Reader, stderr io.Writer, told func(Told) bool) bool {
	lines := bufio.NewReader(stdout)
	for {
		line, err := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			herald, perr := ParseHerald(line)
			switch {
			case perr != nil:
				logf(stderr, "%v", perr)
			case !told(Told{Herald: herald}):
				return false
			}
		}
		if err != nil {
			return true
		}
	}
}

// startError is why a program could not be started, without Go's
// "fork/exec PATH:" wrapping (the program is named where it is told).
func startError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
