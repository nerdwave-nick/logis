// Package wlproto is logis's binding to libwayland-client (spec §5, D5).
//
// For each protocol in protocols/ wayland-scanner generates the C side (a
// client header and the interface tables; libwayland-client carries the
// core protocol's), and wlgen generates the Go side from the same XML: a
// type per interface with a method per request and an On field per event.
// The runtime (Display, Proxy) connects, sends requests through
// wl_proxy_marshal_array_flags and routes every event through one C
// dispatcher (wl_proxy_add_dispatcher) to the Go object a handle names.
//
// A Display and its proxies belong to one goroutine, the window's locked
// owner goroutine, which runs Dispatch (prepare_read / poll / read_events);
// only Wake may be called from elsewhere. Proxies are destroyed explicitly:
// Destroy or a destructor request, a destructor event, or Close for those
// still alive. Handles are released with them; no finalizer is involved.
//
// The generated files are committed, so `go build` needs neither the
// scanner nor wlgen. After adding or updating an XML file, run `go generate
// ./internal/wlproto` (wayland-scanner on PATH) and commit the result; the
// directives live in generate.go.
package wlproto

/*
#cgo pkg-config: wayland-client
*/
import "C"
