package main

// parityCorpus is every rune of Rust logis v1 (src/cli/mod.rs,
// src/cli/dmenu.rs) with good, bad, missing and inline values, their
// orders and conflicts, and every way a session cannot be served.
func parityCorpus() []parityCase {
	var cases []parityCase
	add := func(args ...string) { cases = append(cases, parityCase{args: args}) }
	in := func(stdin string, args ...string) { cases = append(cases, parityCase{args: args, stdin: stdin}) }
	with := func(env []string, args ...string) { cases = append(cases, parityCase{args: args, env: env}) }
	config := func(text string, args ...string) {
		cases = append(cases, parityCase{args: args, files: map[string]string{"config/logis/logis.jsonc": text}})
	}

	// Commands, and what is no command at all.
	add()
	add("--version")
	add("-V")
	add("--version", "extra")
	add("-V", "--version")
	add("--version", "--dmenu")
	add("--dmenu", "--version")
	add("--heresy")
	add("-x")
	add("--")
	add("-")
	add("")
	add("help")
	add("--help")
	add("-h")
	add("--debug-panel")
	add("--dmenu", "--debug-panel")
	add("--detach")
	add("--idle-timeout", "5")
	add("hex:2d2d68ff72657379")
	add("hex:2d2d68c3")
	add("--apps")
	add("--apps", "--apps")
	add("--apps", "--dmenu")
	add("--dmenu", "--apps")
	add("--apps", "--servitor")
	add("--servitor", "--apps")
	add("--servitor")
	add("--servitor", "--dmenu")
	add("--dmenu", "--servitor")
	add("--servitor", "--servitor")
	add("--provider")
	add("--provider", "true")
	add("--provider", "x", "--servitor")
	add("--dmenu", "--provider", "x")
	add("--prompt", "p", "--provider", "x")
	add("--servitor", "--provider", "x")
	add("--config", "/c", "--provider", "rite", "--dmenu", "-x")
	add("-i")
	add("-i", "-i")
	add("--dmenu", "--dmenu")
	add("--dmenu", "--dmenu0")
	add("--dmenu0", "--dmenu")
	add("-d", "-d")
	add("--dmenu=x")
	add("--dmenu0=x")
	add("--apps=x")
	add("--version=x")
	add("-V=x")
	add("daemon")
	add("--dmenu", "daemon")
	add("--apps", "daemon")
	add("provide")
	add("--dmenu", "provide")

	// The daemon's runes.
	add("daemon", "daemon")
	add("daemon", "--bogus")
	add("daemon", "--debug")
	add("daemon", "--dmenu")
	add("daemon", "--version")
	add("daemon", "-V")
	add("daemon", "--detach")
	add("daemon", "--detach", "--detach")
	add("daemon", "--debug-panel")
	add("daemon", "--detach=1")
	add("daemon", "--debug-panel=1")
	add("daemon", "--idle-timeout")
	add("daemon", "--idle-timeout=")
	add("daemon", "--idle-timeout=600")
	add("daemon", "--idle-timeout", "0")
	add("daemon", "--idle-timeout", "0.25")
	for _, v := range []string{"soon", "-1", "-0", "NaN", "nan", "inf", "-inf", "infinity", "", " 5", "5 ", "1e400",
		"1e19", "18446744073709551616", "0x10", "1_0", "+5", ".5", "5.", "1e3", "1E3", "0.0000000001", "１"} {
		add("daemon", "--idle-timeout", v)
	}
	add("daemon", "--idle-timeout", "5", "--idle-timeout", "soon")
	add("daemon", "--idle-timeout", "--config", "x")
	add("daemon", "--config")
	add("daemon", "--config=")
	add("daemon", "--config", "")
	add("daemon", "--config", "a", "--config", "b")
	add("daemon", "--config=a", "--config=b")
	add("daemon", "--bogus", "--config")
	add("daemon", "--config", "x", "--bogus")
	add("daemon", "--config", "x", "--idle-timeout", "soon")
	add("daemon", "--idle-timeout", "soon", "--config")
	add("daemon", "--config", "missing.jsonc")
	add("daemon", "--debug-panel", "--config", "missing.jsonc")
	add("daemon", "hex:ff")

	// Built-in providers.
	add("provide", "apps")
	add("provide", "")
	add("provide", "--servitor")
	add("provide", "servitor", "x")
	add("provide", "servitor", "--config", "x")
	add("provide", "hex:ff")

	// --check-config and --config.
	add("--check-config")
	add("--check-config", "--check-config")
	add("--check-config", "--index")
	add("--check-config", "--dmenu")
	add("--dmenu", "--check-config")
	add("--check-config", "--apps")
	add("--check-config", "--config", "missing.jsonc")
	add("--check-config", "--config=missing.jsonc")
	add("--config=missing.jsonc", "--check-config")
	add("--check-config", "--config", "/nowhere/logis.jsonc")
	add("--config")
	add("--config=")
	add("--config", "")
	add("--config", "a", "--config", "b")
	add("--dmenu", "--config")
	add("--version", "--config", "x")
	add("--config", "x", "--version")
	add("--config", "missing.jsonc")
	add("--config", "missing.jsonc", "--dmenu")
	add("--config", "--dmenu")
	add("--prompt", "--config", "x")
	config(`{}`, "--check-config")
	config(`// a comment, then nothing`, "--check-config")
	config(`{"theme": "cogitator-phosphor", "idle-timeout": 30, "focus-loss": "keep"}`, "--check-config")
	config(`{"heresy": 1}`, "--check-config")
	config(`{"theme": 3}`, "--check-config")
	config(`{"theme": "nowhere"}`, "--check-config")
	config(`{"idle-timeout": -1}`, "--check-config")
	config(`{"focus-loss": "flee"}`, "--check-config")
	config(`{"theme": "forge-crimson-brass",}`, "--check-config")
	config(`{"theme": "forge-crimson-brass" "terminal": "foot"}`, "--check-config")
	config(`{"theme": "a", "theme": "b"}`, "--check-config")
	config(`[]`, "--check-config")
	config(`{`, "--check-config")
	config(`{"heresy": 1}`, "daemon")
	config(`{"heresy": 1}`, "--dmenu")

	// The dmenu runes without --dmenu.
	for _, args := range [][]string{
		{"--prompt", "x"}, {"-p", "x"}, {"--prompt-only", "x"}, {"--placeholder", "x"}, {"--password"},
		{"--lines", "3"}, {"-l", "3"}, {"--width", "3"}, {"-w", "3"}, {"--counter"}, {"--search", "x"},
		{"--select", "x"}, {"--select-index", "1"}, {"--only-match"}, {"--no-run-if-empty"}, {"-R"},
		{"--index"}, {"--with-nth", "1"}, {"--accept-nth", "1"}, {"--match-nth", "1"}, {"--nth-delimiter", ","},
		{"--index", "--counter"}, {"--apps", "--prompt", "x"}, {"--servitor", "--prompt", "x"},
		{"--prompt", "x", "--apps"}, {"--version", "--index"}, {"--index", "--version"},
		{"--lines", "x"}, {"--prompt"}, {"hex:2d2d70726f6d7074", "hex:ff"},
	} {
		add(args...)
	}

	// The dmenu runes with --dmenu: values bad, missing and inline.
	for _, args := range [][]string{
		{"--prompt"}, {"-p"}, {"--prompt-only"}, {"--placeholder"}, {"--search"}, {"--select"},
		{"--select-index"}, {"--lines"}, {"-l"}, {"--width"}, {"-w"}, {"--with-nth"}, {"--accept-nth"},
		{"--match-nth"}, {"--nth-delimiter"},
		{"--lines", "x"}, {"--lines", "-1"}, {"--lines", ""}, {"--lines=x"}, {"--lines", "4294967296"},
		{"--lines", "1.5"}, {"--lines", " 3"}, {"--lines", "0x3"}, {"-l", "hex:ff"},
		{"--width", "0"}, {"--width", "+0"}, {"--width=0"}, {"--width", "x"}, {"--width", "-3"},
		{"--width", "4294967296"},
		{"--select-index", "x"}, {"--select-index", "-1"}, {"--select-index", "1.0"},
		{"--select", "a", "--select-index", "1"}, {"--select-index", "1", "--select", "a"},
		{"--password=ab"}, {"--password=éé"}, {"--password", "x"},
		{"--nth-delimiter", "ab"}, {"--nth-delimiter", "é"}, {"--nth-delimiter", ""}, {"--nth-delimiter="},
		{"--nth-delimiter", "hex:ff"},
		{"--with-nth", "0", "--accept-nth", "x"}, {"--with-nth", "x"}, {"--with-nth", ""}, {"--with-nth", "{0}"},
		{"--with-nth", "{2..1}"}, {"--with-nth", "{1"}, {"--with-nth", "a{1}b{x}"}, {"--with-nth", "-1"},
		{"--accept-nth", "{0..}"}, {"--accept-nth", "{1..0}"}, {"--match-nth", "{}"}, {"--match-nth", "free text"},
		{"--accept-nth", "4294967296"}, {"--with-nth", "{4294967296}"},
		{"-p=x"}, {"-l=3"}, {"-R=1"},
	} {
		add(append([]string{"--dmenu"}, args...)...)
	}

	// Valid dmenu sessions, which then need a daemon and a Wayland session.
	for _, args := range [][]string{
		{"--dmenu"}, {"-d"}, {"--dmenu0"}, {"-d", "-i"}, {"-i", "--dmenu"},
		{"--index", "--dmenu", "-i", "--lines", "3"},
		{"--dmenu", "--counter=x", "--index=y", "--only-match=z", "--no-run-if-empty=w"},
		{"--dmenu", "--lines", "+3", "--width=+7"}, {"--dmenu", "--password"}, {"--dmenu", "--password="},
		{"--dmenu", "--password=é"}, {"--dmenu", "--prompt-only", "pin: "},
		{"--dmenu", "--with-nth", "{2} {1..}", "--accept-nth", "2", "--match-nth", "0", "--nth-delimiter", ","},
		{"--dmenu", "--select", "a", "--select", "b"}, {"--dmenu", "--select-index", "1", "--select-index", "2"},
		{"--dmenu", "--prompt", "-p"}, {"--dmenu", "--prompt", "--dmenu"}, {"--dmenu", "--prompt="},
		{"--dmenu", "--config", "x"}, {"--config=x", "--dmenu"},
	} {
		add(args...)
	}
	in("", "--dmenu", "--no-run-if-empty")
	in("", "--dmenu", "-R")
	in("", "--dmenu0", "-R")
	in("\n", "--dmenu", "-R")
	in("a\n", "--dmenu", "-R")
	in("a\nb\n", "--dmenu")
	in("", "--dmenu", "--prompt-only", "x", "-R")

	// No session to serve: every command, every reason.
	commands := [][]string{nil, {"--apps"}, {"--dmenu"}, {"--servitor"}, {"--provider", "true"},
		{"daemon"}, {"daemon", "--detach"}, {"daemon", "--debug-panel"}}
	for _, env := range [][]string{
		{"WAYLAND_DISPLAY=wayland-nowhere"},
		{"WAYLAND_DISPLAY=$DIR/nowhere"},
		{"WAYLAND_DISPLAY=wayland-nowhere", "XDG_RUNTIME_DIR="},
		{"WAYLAND_DISPLAY=wayland-nowhere", "XDG_RUNTIME_DIR=run"},
		{"XDG_RUNTIME_DIR="},
	} {
		for _, args := range commands {
			with(env, args...)
		}
	}
	// A compositor the window cannot use: every command, and without a
	// runtime dir (the display then given as an absolute path).
	for _, name := range []string{"no-layer-shell", "no-fractional-scale"} {
		for _, env := range [][]string{nil, {"WAYLAND_DISPLAY=$DIR/run/wayland-test", "XDG_RUNTIME_DIR="}} {
			for _, args := range commands {
				cases = append(cases, parityCase{args: args, env: env, compositor: name})
			}
		}
	}
	// Sessions served end to end by the scripted resolver (it picks the
	// first item whose shown text starts with '>', or dismisses) in a daemon
	// the client starts and that idles out a second after.
	scripted := func(stdin string, args ...string) {
		cases = append(cases, parityCase{args: args, stdin: stdin, compositor: "everything",
			env: []string{"LOGIS_RESOLVER=scripted"}, files: map[string]string{"config/logis/logis.jsonc": `{"idle-timeout": 1}`}})
	}
	scripted("a\n>b\nc\n", "--dmenu")
	scripted("a\nb\n", "--dmenu")
	scripted(">a\n>b", "--dmenu")
	scripted("a\n>b\n", "--dmenu", "--index")
	scripted("a\x00>b\nc\x00d", "--dmenu0")
	scripted("x\t>y\tz\n", "--dmenu", "--with-nth", "2")
	scripted("x\t>y\tz\n", "--dmenu", "--with-nth", "2", "--accept-nth", "{3} {1}")
	scripted("x,>y,z\n", "--dmenu", "--nth-delimiter", ",", "--with-nth", "{2..}", "--accept-nth", "1")
	scripted(">a\tb\n", "--dmenu", "--match-nth", "2", "--index")
	scripted(">a\n", "--dmenu", "--prompt-only", "pin: ")
	scripted(">a\n", "-i", "--dmenu", "--counter", "--lines=3", "-p", "rite> ", "--only-match")

	with([]string{"WAYLAND_DISPLAY=wayland-nowhere"}, "--version")
	with([]string{"WAYLAND_DISPLAY=wayland-nowhere"}, "--check-config")
	with([]string{"XDG_CONFIG_HOME=", "HOME="}, "--check-config")
	with([]string{"XDG_CONFIG_HOME=relative"}, "--check-config")
	return cases
}
