// Package provider is what feeds a session (feat/logis §4.8; Rust
// src/provider): the built-in dmenu provider streams a client's stdin;
// external providers are processes speaking JSON lines over their stdin and
// stdout (wire.go, docs/provider-protocol.md). Both drive a session through
// the same Steps and get the user's Answers back.
package provider

import (
	"time"

	"github.com/nerdwave-nick/logis/internal/list"
)

// StepKind is what a provider asks of its session's window.
type StepKind int

const (
	// StepStage: a new list (Options, Entries). The first one shows the
	// window, a later one replaces the list in the same window (the next
	// stage of a flow).
	StepStage StepKind = iota
	// StepAppend: Entries after the last.
	StepAppend
	// StepUpdate: Updates replace items in their place.
	StepUpdate
	// StepRemove: the items at Removed are gone for good.
	StepRemove
	// StepPreselect: select item Index once it is settled, unless the user
	// has moved.
	StepPreselect
	// StepToast: a short Notice over the list, shown for Span.
	StepToast
	// StepReady: the current stage takes Return again after an answer.
	StepReady
)

// Step is one thing a provider asks of its window. Items are addressed by
// index: the order they first arrived in the current stage. Only the
// fields of its Kind are set.
type Step struct {
	Kind    StepKind
	Options list.Options
	Entries []list.Entry
	Updates []Update
	Removed []uint32
	Index   uint32
	Notice  list.Notice
	Span    time.Duration
}

// Update is an item replaced in its place.
type Update struct {
	Index uint32
	Entry list.Entry
}

// AnswerKind is how the user answered the current stage.
type AnswerKind int

const (
	// AnswerItem: Return on the item at Index.
	AnswerItem AnswerKind = iota
	// AnswerTyped: Return on the typed Text, which matched no item.
	AnswerTyped
	// AnswerDismissed: Escape, focus loss, or the compositor closed the
	// window.
	AnswerDismissed
	// AnswerDescend: Shift+Return on the item at Index: its next stage
	// where it has one (an app's desktop actions); elsewhere the same as
	// AnswerItem.
	AnswerDescend
)

// Answer is the user's answer to the visible session.
type Answer struct {
	Kind  AnswerKind
	Index int
	Text  string
}
