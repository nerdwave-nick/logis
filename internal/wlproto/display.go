package wlproto

/*
#include <errno.h>
#include <poll.h>
#include <stdlib.h>
#include "glue.h"
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Display is a connection to the compositor, owned by one goroutine (for
// logis the window's locked owner goroutine): every method except Wake is
// called from it. Close releases the connection and every proxy still
// alive; nothing is left to a finalizer.
type Display struct {
	c    *C.struct_wl_display
	live map[uintptr]Object // guarded by objects
	wake int                // eventfd Wake writes to

	mu     sync.Mutex // guards closed, for Wake
	closed bool
}

// Error is a broken connection: a failed connect, read or flush, a protocol
// error, or the compositor hanging up.
type Error struct {
	Op  string
	Err error
}

func (e *Error) Error() string { return e.Op + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ConnectFd makes a display of a socket already connected to the
// compositor. The display owns fd from now on, also on failure.
func ConnectFd(fd int) (*Display, error) {
	c, err := C.wl_display_connect_to_fd(C.int(fd))
	if c == nil {
		_ = syscall.Close(fd)
		return nil, &Error{"connect", errnoOr(err, syscall.ECONNREFUSED)}
	}
	return newDisplay(c)
}

// ConnectEnv connects the way libwayland does by itself: WAYLAND_SOCKET
// (a socket the compositor handed over), else WAYLAND_DISPLAY.
func ConnectEnv() (*Display, error) {
	c, err := C.wl_display_connect(nil)
	if c == nil {
		return nil, &Error{"connect", errnoOr(err, syscall.ECONNREFUSED)}
	}
	return newDisplay(c)
}

func newDisplay(c *C.struct_wl_display) (*Display, error) {
	wake, err := C.logis_wl_eventfd()
	if wake < 0 {
		C.wl_display_disconnect(c)
		return nil, &Error{"eventfd", errnoOr(err, syscall.EMFILE)}
	}
	return &Display{c: c, live: map[uintptr]Object{}, wake: int(wake)}, nil
}

func errnoOr(err, fallback error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno
	}
	return fallback
}

// Ptr is the wl_display*, for EGL's Wayland platform.
func (d *Display) Ptr() unsafe.Pointer { return unsafe.Pointer(d.c) }

// child sends one of wl_display's two constructor requests.
func (d *Display) child(opcode uint32, iface *Interface) Object {
	p := Proxy{c: (*C.struct_wl_proxy)(unsafe.Pointer(d.c)), display: d}
	var a [1]argument
	c := p.marshal(opcode, a[:], iface, p.Version(), 0)
	if c == nil {
		return nil
	}
	return d.register(iface.new(), c)
}

// Registry asks for the global registry (wl_display.get_registry); its
// globals arrive as OnGlobal events.
func (d *Display) Registry() *WlRegistry {
	r, _ := d.child(1, WlRegistryInterface).(*WlRegistry)
	return r
}

// Sync asks for a callback that is done once the compositor has handled
// every request sent before it (wl_display.sync).
func (d *Display) Sync() *WlCallback {
	cb, _ := d.child(0, WlCallbackInterface).(*WlCallback)
	return cb
}

// Roundtrip sends what is queued and dispatches events until the
// compositor has answered all of it.
func (d *Display) Roundtrip() error {
	if C.wl_display_roundtrip(d.c) < 0 {
		return d.failure("roundtrip")
	}
	return nil
}

// Flush sends the queued requests.
func (d *Display) Flush() error {
	if rc, err := C.wl_display_flush(d.c); rc < 0 && !errors.Is(err, syscall.EAGAIN) {
		return d.failure("flush")
	}
	return nil
}

// Dispatch handles the events already queued, or else sends the queued
// requests and waits for events, for Wake, or for timeout (negative: no
// timeout), reads them and handles them: libwayland's prepare_read /
// read_events protocol around one poll.
func (d *Display) Dispatch(timeout time.Duration) error {
	if C.wl_display_prepare_read(d.c) != 0 {
		return d.dispatchPending()
	}
	events := C.short(C.POLLIN)
	if rc, err := C.wl_display_flush(d.c); rc < 0 {
		if !errors.Is(err, syscall.EAGAIN) {
			C.wl_display_cancel_read(d.c)
			return d.failure("flush")
		}
		events |= C.POLLOUT
	}
	ms := C.int(-1)
	if timeout >= 0 {
		ms = C.int((timeout + time.Millisecond - 1) / time.Millisecond)
	}
	var revents, wakeRevents C.short
	rc, err := C.logis_wl_poll(C.wl_display_get_fd(d.c), events, C.int(d.wake), ms, &revents, &wakeRevents)
	if rc < 0 && !errors.Is(err, syscall.EINTR) {
		C.wl_display_cancel_read(d.c)
		return &Error{"poll", errnoOr(err, syscall.EIO)}
	}
	if revents&(C.POLLIN|C.POLLHUP|C.POLLERR) != 0 {
		if C.wl_display_read_events(d.c) < 0 {
			return d.failure("read")
		}
	} else {
		C.wl_display_cancel_read(d.c)
	}
	if wakeRevents&C.POLLIN != 0 {
		var buf [8]byte
		_, _ = syscall.Read(d.wake, buf[:])
	}
	if revents&C.POLLOUT != 0 {
		if err := d.Flush(); err != nil {
			return err
		}
	}
	return d.dispatchPending()
}

func (d *Display) dispatchPending() error {
	if C.wl_display_dispatch_pending(d.c) < 0 {
		return d.failure("dispatch")
	}
	return nil
}

// failure describes why the connection broke: a protocol error the
// compositor raised, or the system error that ended it.
func (d *Display) failure(op string) error {
	var iface *C.struct_wl_interface
	var id C.uint32_t
	if code := C.wl_display_get_protocol_error(d.c, &iface, &id); code != 0 {
		name := "an unknown object"
		if iface != nil {
			name = C.GoString(iface.name)
		}
		return &Error{op, fmt.Errorf("protocol error %d on %s@%d", uint32(code), name, uint32(id))}
	}
	if errno := C.wl_display_get_error(d.c); errno != 0 {
		return &Error{op, syscall.Errno(errno)}
	}
	return &Error{op, syscall.EPIPE}
}

// Wake makes a Dispatch waiting now, or the next one, return. Any
// goroutine may call it, also after Close.
func (d *Display) Wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	var one [8]byte
	binary.NativeEndian.PutUint64(one[:], 1)
	_, _ = syscall.Write(d.wake, one[:])
}

// Live is how many proxies of this display are alive (not destroyed).
func (d *Display) Live() int {
	objects.Lock()
	defer objects.Unlock()
	return len(d.live)
}

// Close destroys every proxy still alive on the client side, disconnects
// and releases the wake eventfd. The display is unusable afterwards.
func (d *Display) Close() {
	objects.Lock()
	live := make([]Object, 0, len(d.live))
	for _, obj := range d.live {
		live = append(live, obj)
	}
	objects.Unlock()
	for _, obj := range live {
		obj.proxy().forget()
	}
	C.wl_display_disconnect(d.c)
	d.c = nil
	d.mu.Lock()
	d.closed = true
	_ = syscall.Close(d.wake)
	d.mu.Unlock()
}

// Err is why the connection broke, or nil while it works: a broken
// connection stays broken, also for whatever else (EGL) shares it.
func (d *Display) Err() error {
	if C.wl_display_get_error(d.c) == 0 {
		return nil
	}
	return d.failure("connection")
}
