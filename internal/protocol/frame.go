package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxFrame is the largest payload either side accepts. Item batches stay far
// below it.
const MaxFrame = 16 << 20

// ErrTruncated: the stream ended inside a frame.
var ErrTruncated = errors.New("the stream ended inside a frame")

// TooLargeError: the peer announced (or we tried to send) a payload above
// MaxFrame.
type TooLargeError struct{ Len int }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("a frame of %d bytes exceeds %d", e.Len, MaxFrame)
}

// MalformedError: the payload is not the JSON message expected.
type MalformedError struct{ Err error }

func (e *MalformedError) Error() string { return "malformed message: " + e.Err.Error() }

func (e *MalformedError) Unwrap() error { return e.Err }

// WriteFrame writes one message as a single frame: a 4-byte big-endian
// payload length, then that many bytes of JSON. Length and payload go out in
// one write so that frames never interleave.
func WriteFrame(w io.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return &MalformedError{err}
	}
	if len(payload) > MaxFrame {
		return &TooLargeError{len(payload)}
	}
	frame := make([]byte, 4, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	_, err = w.Write(append(frame, payload...))
	return err
}

// ReadFrame reads one frame into message. A clean end of stream between
// frames is io.EOF; one inside a frame is ErrTruncated. Any other read error
// is returned as it is.
func ReadFrame(r io.Reader, message any) error {
	payload, err := readPayload(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, message); err != nil {
		return &MalformedError{err}
	}
	return nil
}

// ReadRequest reads one frame as a request, refusing what DecodeRequest
// refuses.
func ReadRequest(r io.Reader, request *Request) error {
	payload, err := readPayload(r)
	if err != nil {
		return err
	}
	return DecodeRequest(payload, request)
}

func readPayload(r io.Reader) ([]byte, error) {
	var header [4]byte
	if n, err := io.ReadFull(r, header[:]); err != nil {
		return nil, ended(n, err)
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > MaxFrame {
		return nil, &TooLargeError{int(size)}
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, ended(1, err)
	}
	return payload, nil
}

// ended maps the end of the stream after n bytes of a frame.
func ended(n int, err error) error {
	switch {
	case n == 0 && err == io.EOF:
		return io.EOF
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return ErrTruncated
	default:
		return err
	}
}
