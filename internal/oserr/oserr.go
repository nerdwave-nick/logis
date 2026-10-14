// Package oserr words operating-system errors as Rust v1's io::Error did,
// for messages that must read as Rust's (caller contract parity).
package oserr

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
)

// Words is err as Rust's io::Error displayed it: strerror's words
// (capitalised, as glibc has them) and the error number, or err's own
// text when it carries no errno.
func Words(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	words := errno.Error()
	if words != "" {
		words = strings.ToUpper(words[:1]) + words[1:]
	}
	return fmt.Sprintf("%s (os error %d)", words, int(errno))
}
