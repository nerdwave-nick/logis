package wlproto

/*
#include <stdlib.h>
#include "glue.h"
*/
import "C"

import (
	"sync"
	"syscall"
	"unsafe"
)

// Interface is a protocol interface: its name, the newest version the
// wrappers know, and libwayland's table for it.
type Interface struct {
	Name    string
	Version uint32
	c       *C.struct_wl_interface
	new     func() Object
}

// Object is a Go-wrapped proxy: every generated type.
type Object interface {
	proxy() *Proxy
	dispatch(opcode uint32, args []argument)
}

// Proxy is the part every generated type shares: the libwayland proxy and
// the handle its events find it by. A proxy is used on its display's owner
// goroutine only and destroyed explicitly (Destroy, or a destructor
// request or event); no finalizer frees it.
type Proxy struct {
	c       *C.struct_wl_proxy
	display *Display
	handle  uintptr
}

func (p *Proxy) proxy() *Proxy { return p }

// Valid reports whether the proxy is still alive (not destroyed).
func (p *Proxy) Valid() bool { return p != nil && p.c != nil }

// ID is the object's protocol id, 0 once destroyed.
func (p *Proxy) ID() uint32 {
	if !p.Valid() {
		return 0
	}
	return uint32(C.wl_proxy_get_id(p.c))
}

// Version is the version the object was created with.
func (p *Proxy) Version() uint32 {
	if !p.Valid() {
		return 0
	}
	return uint32(C.wl_proxy_get_version(p.c))
}

// Ptr is the C proxy (a wl_surface* for EGL, say); nil once destroyed.
func (p *Proxy) Ptr() unsafe.Pointer { return unsafe.Pointer(p.c) }

// objects maps handles to the live Go objects, for the C dispatcher.
var objects = struct {
	sync.Mutex
	byHandle map[uintptr]Object
	next     uintptr
}{byHandle: map[uintptr]Object{}}

// register makes obj the Go side of the C proxy c and routes its events.
func (d *Display) register(obj Object, c *C.struct_wl_proxy) Object {
	p := obj.proxy()
	objects.Lock()
	objects.next++
	p.c, p.display, p.handle = c, d, objects.next
	objects.byHandle[p.handle] = obj
	d.live[p.handle] = obj
	objects.Unlock()
	C.logis_wl_listen(c, C.uintptr_t(p.handle))
	return obj
}

func (p *Proxy) unregister() {
	objects.Lock()
	delete(objects.byHandle, p.handle)
	delete(p.display.live, p.handle)
	objects.Unlock()
	p.c, p.handle = nil, 0
}

// forget destroys the proxy on the client side only (no request).
func (p *Proxy) forget() {
	if !p.Valid() {
		return
	}
	c := p.c
	p.unregister()
	C.wl_proxy_destroy(c)
}

func (p *Proxy) marshal(opcode uint32, args []argument, iface *Interface, version, flags uint32) *C.struct_wl_proxy {
	var ptr *C.union_wl_argument
	if len(args) > 0 {
		ptr = (*C.union_wl_argument)(unsafe.Pointer(&args[0]))
	}
	var ic *C.struct_wl_interface
	if iface != nil {
		ic = iface.c
	}
	return C.logis_wl_marshal(p.c, C.uint32_t(opcode), ic, C.uint32_t(version), C.uint32_t(flags), ptr)
}

// request sends a request without a new object; a destroyed proxy sends
// nothing.
func (p *Proxy) request(opcode uint32, args []argument) {
	if p.Valid() {
		p.marshal(opcode, args, nil, p.Version(), 0)
	}
}

// destructor sends a destructor request, which also frees the proxy.
func (p *Proxy) destructor(opcode uint32, args []argument) {
	if !p.Valid() {
		return
	}
	version := p.Version()
	c := p.c
	p.unregister()
	tmp := Proxy{c: c}
	tmp.marshal(opcode, args, nil, version, C.WL_MARSHAL_FLAG_DESTROY)
}

// create sends a constructor request; version 0 means the parent's.
func (p *Proxy) create(opcode uint32, args []argument, iface *Interface, version uint32) Object {
	if !p.Valid() {
		return nil
	}
	if version == 0 {
		version = p.Version()
	}
	c := p.marshal(opcode, args, iface, version, 0)
	if c == nil {
		return nil
	}
	return p.display.register(iface.new(), c)
}

// wrap takes over an object the compositor created with an event.
func (p *Proxy) wrap(a argument, iface *Interface) Object {
	c := (*C.struct_wl_proxy)(a.ptr())
	if c == nil {
		return nil
	}
	return p.display.register(iface.new(), c)
}

// eventObject is the Go object an event's object argument names, or the
// zero T for none, a destroyed one or one of another type.
func eventObject[T Object](a argument) T {
	var zero T
	h := uintptr(C.logis_wl_handle(a.ptr()))
	objects.Lock()
	obj := objects.byHandle[h]
	objects.Unlock()
	t, ok := obj.(T)
	if !ok {
		return zero
	}
	return t
}

//export logisWlDispatch
func logisWlDispatch(handle C.uintptr_t, opcode C.uint32_t, args *C.union_wl_argument, n C.int) {
	objects.Lock()
	obj := objects.byHandle[uintptr(handle)]
	objects.Unlock()
	if obj == nil {
		return
	}
	var a []argument
	if n > 0 {
		a = unsafe.Slice((*argument)(unsafe.Pointer(args)), int(n))
	}
	obj.dispatch(uint32(opcode), a)
}

// argument is libwayland's union wl_argument.
type argument struct{ v uint64 }

func (a *argument) word() *uint32           { return (*uint32)(unsafe.Pointer(&a.v)) }
func (a *argument) setUint(v uint32)        { *a.word() = v }
func (a *argument) setInt(v int32)          { *a.word() = uint32(v) }
func (a *argument) setFixed(v Fixed)        { *a.word() = uint32(v) }
func (a *argument) setFd(fd int)            { *a.word() = uint32(int32(fd)) }
func (a *argument) setPtr(p unsafe.Pointer) { *(*unsafe.Pointer)(unsafe.Pointer(&a.v)) = p }
func (a *argument) setProxy(p *Proxy) {
	if p.Valid() {
		a.setPtr(unsafe.Pointer(p.c))
	}
}

func (a argument) uint() uint32        { return *a.word() }
func (a argument) int() int32          { return int32(*a.word()) }
func (a argument) fixed() Fixed        { return Fixed(*a.word()) }
func (a argument) fd() int             { return int(int32(*a.word())) }
func (a argument) ptr() unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&a.v)) }
func (a argument) str() string         { return C.GoString((*C.char)(a.ptr())) }

func (a argument) array() []byte {
	arr := (*C.struct_wl_array)(a.ptr())
	if arr == nil || arr.size == 0 {
		return nil
	}
	return C.GoBytes(arr.data, C.int(arr.size))
}

// Fixed is wl_fixed_t: a signed 24.8 fixed-point number.
type Fixed int32

// Float is the number f stands for.
func (f Fixed) Float() float64 { return float64(f) / 256 }

// FixedFrom is the fixed-point number nearest v.
func FixedFrom(v float64) Fixed {
	if v < 0 {
		return Fixed(v*256 - 0.5)
	}
	return Fixed(v*256 + 0.5)
}

func cString(s string) unsafe.Pointer { return unsafe.Pointer(C.CString(s)) }
func freeC(p unsafe.Pointer)          { C.free(p) }
func closeFd(fd int) {
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}
