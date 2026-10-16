package wlproto

/*
#include <poll.h>
#include "glue.h"
*/
import "C"

// watch is a watched fd and what to do when it is readable.
type watch struct {
	fd    int
	ready func()
}

// Watch adds fd to what Dispatch waits on: when it becomes readable,
// Dispatch returns and calls ready on its goroutine, after the compositor
// events read with it are handled (the window's repeat timer). ready must
// consume what made fd readable.
func (d *Display) Watch(fd int, ready func()) {
	d.Unwatch(fd)
	d.watches = append(d.watches, watch{fd, ready})
}

// Unwatch stops watching fd.
func (d *Display) Unwatch(fd int) {
	for i, w := range d.watches {
		if w.fd == fd {
			d.watches = append(d.watches[:i], d.watches[i+1:]...)
			return
		}
	}
}

// pollfds is the display's fd, the wake eventfd and the watched fds.
func (d *Display) pollfds(events C.short) []C.struct_pollfd {
	d.fds = append(d.fds[:0],
		C.struct_pollfd{fd: C.wl_display_get_fd(d.c), events: events},
		C.struct_pollfd{fd: C.int(d.wake), events: C.POLLIN})
	for _, w := range d.watches {
		d.fds = append(d.fds, C.struct_pollfd{fd: C.int(w.fd), events: C.POLLIN})
	}
	return d.fds
}

// runReady calls the handlers of the watched fds poll found readable.
func (d *Display) runReady(fds []C.struct_pollfd) {
	ready := make([]func(), 0, len(fds))
	for _, p := range fds {
		if p.revents&(C.POLLIN|C.POLLHUP|C.POLLERR) == 0 {
			continue
		}
		for _, w := range d.watches {
			if C.int(w.fd) == p.fd {
				ready = append(ready, w.ready)
			}
		}
	}
	for _, f := range ready {
		f()
	}
}
