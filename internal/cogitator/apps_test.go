package cogitator

import (
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// App sessions in the window: answered as a provider's stage is (the
// window waits for the daemon), and an xdg-activation token asked for the
// shown session.

func (r *rig) showApps(id daemon.SessionID, texts ...string) {
	o := list.DefaultOptions()
	o.OnlyMatch = true
	r.ui.show(daemon.Session{ID: id, Mode: protocol.ModeApps, Options: o, Entries: entries(texts...)})
}

func TestReturnOnAnAppAnswersAndTheWindowWaitsForTheDaemon(t *testing.T) {
	r := newRig()
	r.showApps(3, "Anoint", "Bless")
	r.act(input.Action{Kind: input.Accept})
	if len(r.host.answers) != 1 || r.host.answers[0] != (answered{3, daemon.Answer{Kind: daemon.AnswerItem}}) {
		t.Fatalf("answers %+v", r.host.answers)
	}
	if !r.surf.shown {
		t.Error("the window went before the launch")
	}
	r.act(input.Action{Kind: input.Accept})
	if len(r.host.answers) != 1 {
		t.Errorf("Return was taken twice: %+v", r.host.answers)
	}
}

func TestATokenIsAskedForTheShownSessionAndHandedToTheDaemon(t *testing.T) {
	r := newRig()
	r.surf.token = "tok-9"
	r.showApps(3, "Anoint")
	r.ui.activation(3, "forge-anoint")
	if !slices.Equal(r.surf.asked, []string{"forge-anoint"}) {
		t.Errorf("asked %q", r.surf.asked)
	}
	if !slices.Equal(r.host.tokens, []string{"3 tok-9"}) {
		t.Errorf("tokens %q", r.host.tokens)
	}
	r.saw(t, "token 3")
}

func TestASessionNotShownGetsNoToken(t *testing.T) {
	r := newRig()
	r.surf.token = "tok-9"
	r.showApps(3, "Anoint")
	r.ui.activation(4, "forge-anoint")
	if len(r.surf.asked) != 0 || !slices.Equal(r.host.tokens, []string{"4 "}) {
		t.Errorf("asked %q, tokens %q", r.surf.asked, r.host.tokens)
	}
	r.surf.token = ""
	r.ui.activation(3, "forge-anoint")
	if !slices.Equal(r.host.tokens, []string{"4 ", "3 "}) || slices.Contains(r.events, "token 3") {
		t.Errorf("tokens %q, events %q", r.host.tokens, r.events)
	}
}
