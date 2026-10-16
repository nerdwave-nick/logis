package main

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Plain logis's tabbed launcher on the client's side (feat/logis-go
// 8cba2fc9, a divergence from Rust v1): plain logis with servitor on its
// PATH opens the app launcher with servitor mode's provider as its rites'
// tab; without servitor its rites are silent (2aaad4b9); --apps opens the
// launcher alone.

func TestOnlyPlainLogisIsTabbed(t *testing.T) {
	for _, args := range [][]string{{}, {"--config", "/c"}} {
		if c := mustParse(t, args...); c.kind != appsCommand || !c.tabbed {
			t.Errorf("%v: %+v", args, c)
		}
	}
	if c := mustParse(t, "--apps"); c.kind != appsCommand || c.tabbed {
		t.Errorf("--apps: %+v", c)
	}
}

func TestATabbedOpenNamesServitorModesProviderAsItsRites(t *testing.T) {
	found := func(name string) (string, error) {
		if name != "servitor" {
			t.Errorf("looked for %q", name)
		}
		return "/usr/bin/servitor", nil
	}
	missing := func(string) (string, error) { return "", exec.ErrNotFound }
	open := appsOpen(true, found, "/usr/bin/logis")
	if open.Mode != protocol.ModeApps || open.Rites == nil {
		t.Fatalf("mode %s, rites %v", open.Mode, open.Rites != nil)
	}
	argv := open.Rites.Argv
	if len(argv) != 3 || string(argv[0]) != "/usr/bin/logis" || string(argv[1]) != "provide" || string(argv[2]) != "servitor" {
		t.Errorf("rites %q", argv)
	}
	if len(open.Env) == 0 || len(open.Rites.Env) == 0 {
		t.Error("no environment goes with it")
	}
	if open.RitesSilent {
		t.Error("rites silent with servitor found")
	}
	// Without servitor plain logis is tabbed still, its rites silent
	// (2aaad4b9), however the lookup failed.
	for _, lookPath := range []func(string) (string, error){missing, func(string) (string, error) { return "", errors.New("odd") }} {
		for _, self := range []string{"/usr/bin/logis", ""} {
			if open := appsOpen(true, lookPath, self); open.Rites != nil || !open.RitesSilent || len(open.Env) == 0 {
				t.Errorf("without servitor (self %q): rites %v, silent %v", self, open.Rites != nil, open.RitesSilent)
			}
		}
	}
	if open := appsOpen(false, found, "/usr/bin/logis"); open.Rites != nil || open.RitesSilent {
		t.Error("tabs for --apps")
	}
	if open := appsOpen(false, missing, "/usr/bin/logis"); open.RitesSilent {
		t.Error("silent rites for --apps")
	}
	if open := appsOpen(true, found, ""); open.Rites != nil || open.RitesSilent {
		t.Error("rites without a body of its own to run")
	}
}
