// Package wltest is a fake Wayland compositor for tests, speaking just
// enough of the wire protocol: it answers wl_display.get_registry with the
// globals it was given and wl_display.sync with wl_callback.done, records
// which objects clients bind, and hands every other request to a hook that
// may answer with events (file descriptors included). Close hangs up on
// every client, as a compositor that quits or crashes would.
//
// It is the Go port of the Rust tests' support/fake_compositor.rs and is
// imported only by tests.
package wltest

import (
	"encoding/binary"
	"net"
	"path/filepath"
	"sync"
	"syscall"
)

const (
	displayID          = 1
	displaySync        = 0
	displayGetRegistry = 1
	displayDeleteID    = 1
	registryBind       = 0
	registryGlobal     = 0
	registryRemove     = 1
	callbackDone       = 0
)

// Global is one advertised global.
type Global struct {
	Interface string
	Version   uint32
}

// Request is one request a client sent to an object other than wl_display.
type Request struct {
	Object uint32
	Opcode uint16
	// Body is the argument bytes; read them with a Reader.
	Body []byte
	// Fds are the file descriptors that came with the request; the hook
	// owns them.
	Fds []int
}

// Compositor is a running fake compositor.
type Compositor struct {
	Path     string
	globals  []Global
	listener *net.UnixListener

	mu      sync.Mutex
	clients []*Client
	closed  bool
	hook    func(*Client, Request)
}

// Serve listens on dir/name and serves each client on its own goroutine.
func Serve(dir, name string, globals []Global) (*Compositor, error) {
	path := filepath.Join(dir, name)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	c := &Compositor{Path: path, globals: globals, listener: l}
	go c.accept()
	return c, nil
}

// OnRequest sets the hook every request to a non-display object goes to;
// it runs on the client's goroutine.
func (c *Compositor) OnRequest(hook func(*Client, Request)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hook = hook
}

// Close hangs up on every client, now and as soon as one connects.
func (c *Compositor) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	_ = c.listener.Close()
	for _, cl := range c.clients {
		_ = cl.conn.Close()
	}
}

// RemoveGlobal announces to every client's registries that global name
// (1-based, in the order given to Serve) is gone.
func (c *Compositor) RemoveGlobal(name uint32) {
	c.mu.Lock()
	clients := append([]*Client(nil), c.clients...)
	c.mu.Unlock()
	for _, cl := range clients {
		cl.mu.Lock()
		registries := append([]uint32(nil), cl.registries...)
		cl.mu.Unlock()
		for _, r := range registries {
			_ = cl.Send(r, registryRemove, name)
		}
	}
}

// Clients are the clients connected so far.
func (c *Compositor) Clients() []*Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*Client(nil), c.clients...)
}

func (c *Compositor) accept() {
	for {
		conn, err := c.listener.AcceptUnix()
		if err != nil {
			return
		}
		cl := &Client{conn: conn, compositor: c, bound: map[uint32]string{}}
		c.mu.Lock()
		if c.closed {
			_ = conn.Close()
		}
		c.clients = append(c.clients, cl)
		c.mu.Unlock()
		go cl.serve()
	}
}

// Client is one connected client.
type Client struct {
	conn       *net.UnixConn
	compositor *Compositor
	write      sync.Mutex

	mu         sync.Mutex
	bound      map[uint32]string // object id → interface
	registries []uint32
}

// Bound is the id of the most recent object the client bound to iface
// through the registry, or 0.
func (cl *Client) Bound(iface string) uint32 {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	var id uint32
	for object, name := range cl.bound {
		if name == iface && object > id {
			id = object
		}
	}
	return id
}

// Fd marks an int argument of Send as a file descriptor to pass.
type Fd int

// Send writes one event: args are uint32, int32, string, []byte (array) or
// Fd, in wire order.
func (cl *Client) Send(object uint32, opcode uint16, args ...any) error {
	body, fds, err := encode(args)
	if err != nil {
		return err
	}
	msg := make([]byte, 8, 8+len(body))
	binary.NativeEndian.PutUint32(msg, object)
	binary.NativeEndian.PutUint32(msg[4:], uint32(8+len(body))<<16|uint32(opcode))
	msg = append(msg, body...)
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	cl.write.Lock()
	defer cl.write.Unlock()
	_, _, err = cl.conn.WriteMsgUnix(msg, oob, nil)
	return err
}

func (cl *Client) serve() {
	var pending []byte
	var fds []int
	buf := make([]byte, 4096)
	oob := make([]byte, syscall.CmsgSpace(28*4))
	for {
		n, oobn, _, _, err := cl.conn.ReadMsgUnix(buf, oob)
		if err != nil || n == 0 {
			return
		}
		fds = append(fds, rights(oob[:oobn])...)
		pending = append(pending, buf[:n]...)
		for len(pending) >= 8 {
			size := int(binary.NativeEndian.Uint32(pending[4:]) >> 16)
			if size < 8 || len(pending) < size {
				break
			}
			r := Request{
				Object: binary.NativeEndian.Uint32(pending),
				Opcode: uint16(binary.NativeEndian.Uint32(pending[4:])),
				Body:   append([]byte(nil), pending[8:size]...),
			}
			// A request takes the fds that arrived with or before it; the
			// fake does not know signatures, so a hook gets them all.
			r.Fds, fds = fds, nil
			pending = pending[size:]
			if err := cl.handle(r); err != nil {
				return
			}
		}
	}
}

func rights(oob []byte) []int {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil
	}
	var fds []int
	for _, m := range msgs {
		got, err := syscall.ParseUnixRights(&m)
		if err == nil {
			fds = append(fds, got...)
		}
	}
	return fds
}

func (cl *Client) handle(r Request) error {
	rd := Reader{b: r.Body}
	switch {
	case r.Object == displayID && r.Opcode == displayGetRegistry:
		id := rd.Uint()
		cl.mu.Lock()
		cl.registries = append(cl.registries, id)
		cl.mu.Unlock()
		for i, g := range cl.compositor.globals {
			if err := cl.Send(id, registryGlobal, uint32(i+1), g.Interface, g.Version); err != nil {
				return err
			}
		}
		return nil
	case r.Object == displayID && r.Opcode == displaySync:
		id := rd.Uint()
		if err := cl.Send(id, callbackDone, uint32(0)); err != nil {
			return err
		}
		return cl.Send(displayID, displayDeleteID, id)
	case r.Object == displayID:
		return nil
	}
	cl.mu.Lock()
	isRegistry := false
	for _, reg := range cl.registries {
		isRegistry = isRegistry || reg == r.Object
	}
	if isRegistry && r.Opcode == registryBind {
		_ = rd.Uint() // name
		iface := rd.String()
		_ = rd.Uint() // version
		cl.bound[rd.Uint()] = iface
	}
	cl.mu.Unlock()
	cl.compositor.mu.Lock()
	hook := cl.compositor.hook
	cl.compositor.mu.Unlock()
	if hook != nil {
		hook(cl, r)
	} else {
		for _, fd := range r.Fds {
			_ = syscall.Close(fd)
		}
	}
	return nil
}
