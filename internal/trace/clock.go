package trace

import (
	"syscall"
	"unsafe"
)

// clockMonotonic is CLOCK_MONOTONIC (linux/time.h).
const clockMonotonic = 1

// MonotonicNS is CLOCK_MONOTONIC in nanoseconds, the clock harnesses time
// against: the same clock in every process, unlike Go's own monotonic
// readings, which cannot leave the process.
func MonotonicNS() uint64 {
	var ts syscall.Timespec
	if _, _, errno := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, clockMonotonic, uintptr(unsafe.Pointer(&ts)), 0); errno != 0 {
		panic("clock_gettime(CLOCK_MONOTONIC): " + errno.Error())
	}
	return uint64(ts.Sec)*1_000_000_000 + uint64(ts.Nsec)
}
