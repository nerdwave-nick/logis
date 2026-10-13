// Package wlproto holds the C protocol glue that wayland-scanner generates
// from the protocol XML files in protocols/ (spec §5, D5): for each protocol
// a client header (<name>-client-protocol.h) and its interface tables
// (<name>-protocol.c), compiled by cgo against libwayland-client.
//
// The generated files are committed, so `go build` needs no scanner.
// After adding or updating an XML file, regenerate with `go generate
// ./internal/wlproto` (wayland-scanner on PATH) and commit the result; one
// pair of directives per protocol lives in generate.go. The Go-side wrappers
// and the event dispatch come with the libwayland binding ticket.
package wlproto

/*
#cgo pkg-config: wayland-client
#include "viewporter-client-protocol.h"
*/
import "C"
