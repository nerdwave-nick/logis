package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// Ported from Rust src/cli/mod.rs servitor_asks_for_servitor_mode.
func TestServitorAsksForServitorMode(t *testing.T) {
	if c := mustParse(t, "--servitor"); c.kind != servitorCommand || c.config != "" {
		t.Fatalf("%+v", c)
	}
	if c := mustParse(t, "--servitor", "--config", "/c"); c.kind != servitorCommand || c.config != "/c" {
		t.Fatalf("%+v", c)
	}
	if _, err := parse([]string{"--servitor", "--dmenu"}); err == nil {
		t.Fatal("--servitor with --dmenu accepted")
	}
	if c := mustParse(t, "--provider", "x", "--servitor"); c.kind != providerCommand || !reflect.DeepEqual(c.argv, []string{"x", "--servitor"}) {
		t.Fatalf("%+v", c)
	}
	if _, err := parse([]string{"--servitor", "--prompt", "x"}); err == nil ||
		err.Error() != "the rune --prompt serves only --dmenu; this cogitator has no other list yet" {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(knownRunes, "--provider PROGRAM [ARG]..., --servitor, --check-config") {
		t.Fatalf("%s", knownRunes)
	}
}

// Ported from Rust src/cli/mod.rs provide_names_a_built_in_provider.
func TestProvideNamesABuiltInProvider(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"provide"}, "logis: the rune provide stands empty: it wants the name of a built-in provider: servitor\n"},
		{[]string{"provide", "apps"}, "logis: the rune provide cannot bear 'apps': it wants the name of a built-in provider: servitor\n"},
		{[]string{"provide", "servitor", "x"}, "logis: the rune 'x' is unknown to this cogitator. It knows only " + knownRunes + ".\n"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(c.args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.String() != c.want {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", c.args, code, stdout.String(), stderr.String())
		}
	}
}
