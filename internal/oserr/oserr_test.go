package oserr

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestErrnosReadAsTheirDescriptionAlone(t *testing.T) {
	_, err := os.Open("/nonexistent/logis")
	for _, c := range []struct {
		err  error
		want string
	}{
		{syscall.ENOENT, "No such file or directory"},
		{syscall.ECONNREFUSED, "Connection refused"},
		{fmt.Errorf("dial: %w", syscall.EACCES), "Permission denied"},
		{err, "No such file or directory"},
		{errors.New("no errno here"), "no errno here"},
	} {
		if got := Words(c.err); got != c.want {
			t.Errorf("Words(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
