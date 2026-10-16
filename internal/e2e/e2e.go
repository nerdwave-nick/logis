// Package e2e is the end-to-end harness (spec §6): a test compositor the
// harness starts itself, logis clients inside it with the test trace on, a
// virtual keyboard and pointer, screen capture and a selection owner, all
// confined to that compositor.
//
//   - Locally: a nested niri (LOGIS_E2E=niri), opened as a window in the
//     running session (WAYLAND_DISPLAY) with its own generated config (never
//     the owner's, never --session), animations off.
//   - On the CI runner: sway on wlroots' headless backend (LOGIS_E2E=sway),
//     pixman renderer, no input devices, one 1280x720 output at scale 1.
//
// Without LOGIS_E2E the compositor tests skip; with it, a compositor that
// cannot start fails them.
//
// Every compositor runs with a scratch XDG_RUNTIME_DIR of its own, so its
// socket, logis's daemon socket and every client live there. Every
// connection the harness makes (Harness, SelectionOwner, Watchdog) goes
// through ConnectVerified, which refuses a socket whose server is not the
// compositor process the harness spawned (SO_PEERCRED), so keys, pointer
// events and selections can never reach the live session. The harness
// connection also puts an opaque black backdrop under everything, so
// captures look the same in both compositors.
//
// The package is test support: only tests import it, so nothing of it is
// linked into logis.
package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Env selects the test compositor.
const Env = "LOGIS_E2E"

// Patience bounds how long a compositor may take to open its socket, a
// client to report focus or a frame, or a capture to complete.
const Patience = 15 * time.Second

// Kind is a compositor the harness can run.
type Kind int

const (
	// None: no LOGIS_E2E, the compositor tests skip.
	None Kind = iota
	// NestedNiri is niri as a window in the running session (local runs).
	NestedNiri
	// HeadlessSway is sway on wlroots' headless backend (the CI runner).
	HeadlessSway
)

// ParseKind reads a value of Env: niri, sway, or empty for none.
func ParseKind(value string) (Kind, error) {
	switch strings.TrimSpace(value) {
	case "":
		return None, nil
	case "niri":
		return NestedNiri, nil
	case "sway":
		return HeadlessSway, nil
	}
	return None, fmt.Errorf("%s=%s: expected niri or sway", Env, value)
}

// Name is a short name for reports.
func (k Kind) Name() string {
	switch k {
	case NestedNiri:
		return "niri-nested"
	case HeadlessSway:
		return "sway-headless"
	}
	return "none"
}

// Requested is the compositor Env asks for; without one the test is
// skipped, and an unknown value fails it.
func Requested(t testing.TB) Kind {
	t.Helper()
	k, err := ParseKind(os.Getenv(Env))
	if err != nil {
		t.Fatal(err)
	}
	if k == None {
		t.Skipf("SKIPPED: set %s=niri (nested, local) or %s=sway (headless, CI)", Env, Env)
	}
	return k
}

// NiriConfig is the generated niri config (%s: the output scale): no
// animations, no startup overlay, no Xwayland, the injector's keymap (US,
// Menu as Compose) and key repeat (Repeat). Its background is deliberately
// not black: captures must depend only on the harness's own backdrop.
const NiriConfig = `// logis test compositor (nested niri); generated, never the owner's config.
output "winit" {
    scale %s
}
hotkey-overlay {
    skip-at-startup
}
animations {
    off
}
xwayland-satellite {
    off
}
layout {
    background-color "#404040"
}
input {
    keyboard {
        xkb {
            layout "us"
            options "compose:menu"
        }
        repeat-delay 300
        repeat-rate 30
    }
}
prefer-no-csd
`

// SwayConfig is the generated sway config (%s: the output scale): one
// headless output at 1280x720, no Xwayland, no bindings (injected keys must reach
// the client, not sway), keyboards with the injector's keymap and Repeat.
// Without an `output … bg` sway shows its default dark grey, which the
// backdrop covers.
const SwayConfig = `# logis test compositor (headless sway); generated.
output HEADLESS-1 mode 1280x720@60Hz scale %s
xwayland disable
default_border none
focus_follows_mouse no
input type:keyboard {
    xkb_layout us
    xkb_options compose:menu
    repeat_delay 300
    repeat_rate 30
}
`

// Repeat is the key repeat both test compositors configure: rate per
// second, delay in ms. Tests read what the client received from its
// repeat-info trace rather than assuming it.
var Repeat = struct{ Rate, Delay int }{30, 300}

// XCompose is every client's Compose table (XCOMPOSEFILE), so no test
// depends on the machine's locale tables or anyone's ~/.XCompose. The last
// two sequences give what no system table gives: one proves this file is
// the one in use, the other composes one grapheme of two code points.
const XCompose = "# logis test Compose table; generated.\n" +
	"<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis\n" +
	"<Multi_key> <l> <g> : \"λ\"\n" +
	"<Multi_key> <e> <e> : \"é\"\n"

// fontsConf is every client's fontconfig configuration: only the pinned
// fonts in tests/fonts, so text renders the same on every machine; the
// first %s is the fonts directory, the second the cache.
const fontsConf = `<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <dir>%s</dir>
  <cachedir>%s</cachedir>
  <alias binding="same">
    <family>sans-serif</family>
    <prefer><family>Noto Sans</family></prefer>
  </alias>
  <alias binding="same">
    <family>monospace</family>
    <prefer><family>Noto Sans Mono</family></prefer>
  </alias>
  <alias binding="same">
    <family>serif</family>
    <prefer><family>Noto Serif</family></prefer>
  </alias>
</fontconfig>
`
