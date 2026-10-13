// Command wlgen writes package wlproto's Go wrappers for one protocol XML
// file: `wlgen protocols/NAME.xml OUT.go`. The core protocol (wayland.xml)
// includes <wayland-client.h>; every other one the wayland-scanner client
// header NAME-client-protocol.h next to the output.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nerdwave-nick/logis/internal/wlgen"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: wlgen PROTOCOL.xml OUT.go")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "wlgen:", err)
		os.Exit(1)
	}
}

func run(xmlPath, out string) error {
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return err
	}
	p, err := wlgen.Parse(data)
	if err != nil {
		return err
	}
	src, err := wlgen.Generate(p, wlgen.OptionsFor(filepath.ToSlash(xmlPath)))
	if err != nil {
		return err
	}
	return os.WriteFile(out, src, 0o644)
}
