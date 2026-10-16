package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// A frame is a big-endian length and JSON.
func TestAFrameIsABigEndianLengthAndJSON(t *testing.T) {
	var out bytes.Buffer
	if err := WriteFrame(&out, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\x00\x00\x00\x07{\"a\":1}"; got != want {
		t.Errorf("frame %q, want %q", got, want)
	}
}

// Ported from messages_round_trip_in_order_and_end_cleanly.
func TestMessagesRoundTripInOrderAndEndCleanly(t *testing.T) {
	var stream bytes.Buffer
	messages := []any{map[string]any{"type": "hello"}, []any{1.0, "two", nil}, "é\n"}
	for _, m := range messages {
		if err := WriteFrame(&stream, m); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range messages {
		var got any
		if err := ReadFrame(&stream, &got); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !jsonEqual(t, got, want) {
			t.Errorf("frame %d: %v, want %v", i, got, want)
		}
	}
	var none any
	if err := ReadFrame(&stream, &none); err != io.EOF {
		t.Errorf("after the last frame: %v, want io.EOF", err)
	}
}

// Ported from a_stream_ending_inside_a_frame_is_truncated.
func TestAStreamEndingInsideAFrameIsTruncated(t *testing.T) {
	for _, in := range []string{"\x00\x00", "\x00\x00\x00\x07{\"a\""} {
		var v any
		if err := ReadFrame(strings.NewReader(in), &v); !errors.Is(err, ErrTruncated) {
			t.Errorf("%q: %v, want ErrTruncated", in, err)
		}
	}
}

// Ported from an_oversized_length_is_refused_before_reading_the_payload.
func TestAnOversizedLengthIsRefusedBeforeReadingThePayload(t *testing.T) {
	header := []byte{0x01, 0x00, 0x00, 0x01} // MaxFrame + 1
	var v any
	err := ReadFrame(bytes.NewReader(header), &v)
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Len != MaxFrame+1 {
		t.Fatalf("%v, want a TooLargeError of %d", err, MaxFrame+1)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("message %q", err)
	}
}

// Ported from an_oversized_message_is_not_sent.
func TestAnOversizedMessageIsNotSent(t *testing.T) {
	var out bytes.Buffer
	var tooLarge *TooLargeError
	if err := WriteFrame(&out, strings.Repeat("x", MaxFrame)); !errors.As(err, &tooLarge) {
		t.Errorf("%v, want a TooLargeError", err)
	}
	if out.Len() != 0 {
		t.Errorf("%d bytes were sent", out.Len())
	}
}

// Ported from a_payload_that_is_not_json_is_malformed.
func TestAPayloadThatIsNotJSONIsMalformed(t *testing.T) {
	var v any
	err := ReadFrame(strings.NewReader("\x00\x00\x00\x03{x}"), &v)
	var malformed *MalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("%v, want a MalformedError", err)
	}
	if !strings.HasPrefix(err.Error(), "malformed message: ") {
		t.Errorf("message %q", err)
	}
}

// One frame is one write, so that frames of two writers never interleave.
func TestAFrameGoesOutInOneWrite(t *testing.T) {
	var w countingWriter
	if err := WriteFrame(&w, Hello()); err != nil {
		t.Fatal(err)
	}
	if w.writes != 1 {
		t.Errorf("%d writes, want 1", w.writes)
	}
}

type countingWriter struct{ writes int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return len(p), nil
}
