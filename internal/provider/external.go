package provider

import (
	"errors"
	"fmt"
	"syscall"
)

// One session's external provider: the protocol's rules on top of a running
// process. It greets the provider, checks its greeting, keeps its stages
// and maps the user's answers to its lines.
//
// When the session ends without the provider's close, logis says cancel and
// closes the provider's input. Whether logis may then also signal it
// decides how it is retired: a provider that was never sent an activate or
// submit cannot have started an action, so it is ended if it lingers; one
// that was may be carrying out an action, which dismissal must never kill,
// so it is never signalled (feat/logis §4.8, story 10).

// Running is the process side of a provider, as its session uses it.
type Running interface {
	// Send sends one line; an error ends the session.
	Send(m ToProvider) error
	// CloseInput closes its input: it reads end of file.
	CloseInput()
	// Signal signals the provider alone (never its group or children).
	Signal(sig syscall.Signal)
}

// Said is what a provider's line means for the session: steps for the
// window (perhaps none), or Close, the end of its flow.
type Said struct {
	Steps []Step
	Close bool
}

// External is an external provider in one session.
type External struct {
	// Log takes a line for the daemon's log (a skipped unknown line).
	Log     func(message string)
	running Running
	stages  Stages
	greeted bool
	// acted: an activate or submit went out; an action may be under way.
	acted bool
	// closed: the provider said close.
	closed bool
	// exited: the process is gone.
	exited bool
}

// NewExternal greets a started provider.
func NewExternal(r Running) (*External, error) {
	if err := r.Send(Greeting()); err != nil {
		return nil, err
	}
	return &External{running: r, Log: func(string) {}}, nil
}

// Heard is one line from the provider: its message, or why it could not be
// read.
func (e *External) Heard(m Message, err error) (Said, error) {
	if err != nil {
		return Said{}, err
	}
	if u, ok := m.(Unknown); ok {
		e.Log(fmt.Sprintf("a provider's '%s' line is unknown; skipped", u.Type))
		return Said{}, nil
	}
	hello, isHello := m.(Hello)
	switch {
	case !e.greeted && isHello && hello.Protocol == Protocol:
		e.greeted = true
		return Said{}, nil
	case !e.greeted && isHello:
		return Said{}, fmt.Errorf("the provider speaks protocol %d; this cogitator speaks %d", hello.Protocol, Protocol)
	case !e.greeted:
		return Said{}, errors.New("the provider did not greet first ('hello')")
	case isHello:
		return Said{}, errors.New("the provider greeted twice")
	}
	if _, ok := m.(Close); ok {
		e.closed = true
		return Said{Close: true}, nil
	}
	steps, err := e.stages.Apply(m)
	return Said{Steps: steps}, err
}

// Answer: the user answered the current stage (not a dismissal).
func (e *External) Answer(a Answer) error {
	line, ok := e.stages.Answer(a)
	if !ok {
		return nil
	}
	e.acted = true
	return e.running.Send(line)
}

// Exited: the process is gone.
func (e *External) Exited() { e.exited = true }

// Retire: the session is over. It says cancel unless the provider closed it
// (or is gone), closes its input, and returns the process if logis may
// still end it (see above).
func (e *External) Retire() (Running, bool) {
	if !e.closed && !e.exited {
		_ = e.running.Send(Cancel{})
	}
	e.running.CloseInput()
	if e.acted || e.exited {
		return nil, false
	}
	return e.running, true
}
