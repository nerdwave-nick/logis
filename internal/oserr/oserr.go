// Package oserr words operating-system errors as "<description> (os
// error N)", for messages whose wording is part of the caller contract.
package oserr

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
)

// Words is err as "<description> (os error N)": strerror's words
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
