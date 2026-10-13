package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Provider sessions end to end (Rust tests/provider_window.rs): a real
// daemon in the test compositor, `logis --provider` running the fixture
// provider tests/fixtures/providers/forge.sh, and keys from the harness's
// virtual keyboard. Runs only with LOGIS_E2E=niri|sway. Waits follow the
// daemon's test trace.

func TestE2EATwoStageFlowWithAPendingRowEndsInAToast(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")
	client := c.ProviderWith([]string{"FORGE_DIR=" + dir, "FORGE_GATE=" + gate}, forge(t), "flow")

	// The rite stage: its first row is still pending, so Return does
	// nothing. Down and Up after it prove the key was handled.
	focused := c.Shown(2)
	// The first stage sizes the window (forge.sh asks for 6 lines, 44 wide).
	o := list.DefaultOptions()
	lines, width := uint32(6), uint32(44)
	o.Lines, o.Width = &lines, &width
	th := theme.Default()
	size, _ := scene.Fit(o, &th)
	checkWindow(t, c.Harness, "provider-pending", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyEnter)
	c.Tap(focused, e2e.KeyDown)
	if got := c.Next("select"); got != "1" {
		t.Fatalf("select %s", got)
	}
	c.Tap(focused, e2e.KeyUp)
	if got := c.Next("select"); got != "0" {
		t.Fatalf("select %s", got)
	}
	if slices.ContainsFunc(c.Daemon.Events(), func(e trace.Event) bool { return strings.HasPrefix(e.What, "accept") }) {
		t.Fatal("a pending row was accepted")
	}

	// It settles; now Return takes it to the aspect stage, in the same window.
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.Next("update"); got != "0" {
		t.Fatalf("update %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "0" {
		t.Fatalf("accept %s", got)
	}
	c.Next("stage")
	if got := c.Next("items"); got != "2" {
		t.Fatalf("items %s", got)
	}

	// Choosing an aspect: the provider toasts and closes; the caller is
	// answered at once, the window lingers on the toast without the
	// keyboard, then goes.
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "0" {
		t.Fatalf("accept %s", got)
	}
	if got := c.Next("toast"); got != `"the rite of oil is begun"` {
		t.Fatalf("toast %s", got)
	}
	c.Next("linger")
	checkWindow(t, c.Harness, "provider-toast", int(size.W), int(size.H))
	if ended := client.Ended(); ended.Code != 0 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	c.Next("hide")
	if heard := heardBy(dir); !strings.Contains(heard, `{"type":"activate","stage":"aspect","id":"on"}`) {
		t.Fatalf("%q", heard)
	}
}

func TestE2EEscapeAfterAnActivationNeverKillsTheActionItStarted(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	dir := t.TempDir()
	client := c.ProviderWith([]string{"FORGE_DIR=" + dir}, forge(t), "act")
	focused := c.Shown(1)
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "0" {
		t.Fatalf("accept %s", got)
	}
	action, provider := pidIn(t, dir, "action"), pidIn(t, dir, "provider")
	defer func() {
		_ = syscall.Kill(action, syscall.SIGKILL)
		_ = syscall.Kill(provider, syscall.SIGKILL)
	}()
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	// Past both graces a provider that never acted would get.
	time.Sleep(2500 * time.Millisecond)
	if !alive(action) {
		t.Fatal("dismissal killed the action")
	}
	if !alive(provider) {
		t.Fatal("dismissal signalled the provider that started it")
	}
}

func TestE2EARowsIconIsDrawnInPlaceOfItsGlyph(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	dir := t.TempDir()
	icon, err := filepath.Abs("../../tests/fixtures/icons/share/icons/logis-forge/32x32/apps/both-formats.png")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "icons.sh")
	body := "#!/bin/sh\nread -r hello\necho '{\"type\":\"hello\",\"protocol\":1}'\n" +
		"echo '{\"type\":\"stage\",\"id\":\"s\",\"lines\":3,\"width\":30,\"items\":[" +
		"{\"id\":\"a\",\"columns\":[\"an icon\"],\"icon\":\"" + icon + "\",\"glyph\":\"G\"}," +
		"{\"id\":\"b\",\"columns\":[\"no icon found\"],\"icon\":\"logis-nowhere\",\"glyph\":\"\u2020\"}]}'\n" +
		"while read -r line; do case $line in *cancel*) exit 0 ;; esac; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	client := c.ProviderWith(nil, script)
	focused := c.Shown(2)
	o := list.DefaultOptions()
	lines, width := uint32(3), uint32(30)
	o.Lines, o.Width = &lines, &width
	th := theme.Default()
	size, _ := scene.Fit(o, &th)
	checkWindow(t, c.Harness, "provider-icons", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyEsc)
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
}
