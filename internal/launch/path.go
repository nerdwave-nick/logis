package launch

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// xOK is access(2)'s X_OK (POSIX), which package syscall does not name.
const xOK = 0x1

// LookPath is where program is found: itself if it is an absolute path to
// an executable file, else the first executable file of that name in one of
// pathVar's absolute directories (pathVar is a PATH value, ':'-separated).
// A program with a '/' that is not absolute is never found.
func LookPath(pathVar, program string) (string, bool) {
	if strings.Contains(program, "/") {
		if filepath.IsAbs(program) && executable(program) {
			return program, true
		}
		return "", false
	}
	if program == "" {
		return "", false
	}
	for _, dir := range filepath.SplitList(pathVar) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, program); executable(p) {
			return p, true
		}
	}
	return "", false
}

// executable says whether path is a file this process may execute.
func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && syscall.Access(path, xOK) == nil
}
