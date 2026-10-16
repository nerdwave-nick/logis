package oserr

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestErrnosReadAsTheirDescriptionAndNumber(t *testing.T) {
	_, err := os.Open("/nonexistent/logis")
	for _, c := range []struct {
		err  error
		want string
	}{
		{syscall.ENOENT, "No such file or directory (os error 2)"},
		{syscall.ECONNREFUSED, "Connection refused (os error 111)"},
		{fmt.Errorf("dial: %w", syscall.EACCES), "Permission denied (os error 13)"},
		{err, "No such file or directory (os error 2)"},
		{errors.New("no errno here"), "no errno here"},
	} {
		if got := Words(c.err); got != c.want {
			t.Errorf("Words(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
