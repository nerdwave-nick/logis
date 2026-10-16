// Package oserr words operating-system errors for messages: the C
// library's description of the error, without the call or path Go wraps
// around it.
package oserr

import (
	"errors"
	"strings"
	"syscall"
)

// Words is err as its description: strerror's words, capitalised as glibc
// has them ("No such file or directory"), or err's own text when it
// carries no errno.
func Words(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	words := errno.Error()
	if words != "" {
		words = strings.ToUpper(words[:1]) + words[1:]
	}
	return words
}
