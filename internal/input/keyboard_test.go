package input

import (
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
)

// Tests ported from Rust src/input/keyboard.rs.

func keyboard(t *testing.T) *Keyboard {
	t.Helper()
	k := NewKeyboard(testCompose(t))
	k.SetKeymap(keymap(t, "us"))
	k.SetRepeatRate(25, 300)
	return k
}

func wantAction(t *testing.T, got Action, ok bool, want Action) {
	t.Helper()
	if !ok || got != want {
		t.Errorf("%+v %v, want %+v", got, ok, want)
	}
}

func wantNothing(t *testing.T, got Action, ok bool) {
	t.Helper()
	if ok {
		t.Errorf("%+v, want nothing", got)
	}
}

// shifted is a press with the masks a compositor sends around it.
func shifted(k *Keyboard, code uint32, now time.Time) (Action, bool) {
	k.Press(keyShiftL, now)
	k.SetMask(mShift, 0, 0, 0)
	a, ok := k.Press(code, now)
	k.Release(code)
	k.Release(keyShiftL)
	k.SetMask(0, 0, 0, 0)
	return a, ok
}

func TestWithoutAKeymapKeysDoNothing(t *testing.T) {
	k := NewKeyboard(nil)
	a, ok := k.Press(keyA, time.Now())
	wantNothing(t, a, ok)
}

func TestTheComposeKeyWithARealShiftTypesOUmlaut(t *testing.T) {
	k, t0 := keyboard(t), time.Now()
	a, ok := k.Press(keyCompose, t0)
	wantNothing(t, a, ok)
	a, ok = k.Press(keyO, t0)
	wantNothing(t, a, ok)
	a, ok = shifted(k, keyQuote, t0)
	wantAction(t, a, ok, Insert("ö"))
	if _, held := k.Deadline(); held {
		t.Error("composed keys repeat")
	}
}

func TestKeysTypeTextAndShortcutsAct(t *testing.T) {
	k, t0 := keyboard(t), time.Now()
	a, ok := k.Press(keyA, t0)
	wantAction(t, a, ok, Insert("a"))
	a, ok = shifted(k, keyA, t0)
	wantAction(t, a, ok, Insert("A"))
	a, ok = k.Press(keyEsc, t0)
	wantAction(t, a, ok, ActDismiss)
	k.SetMask(mControl, 0, mLock, 0)
	a, ok = k.Press(keyN, t0)
	wantAction(t, a, ok, ActNext) // Caps Lock on
}

func TestCtrlGWorksOnASecondLayout(t *testing.T) {
	k := keyboard(t)
	k.SetKeymap(keymap(t, "us,ru"))
	k.SetMask(mControl, 0, 0, 1)
	a, ok := k.Press(keyG, time.Now())
	wantAction(t, a, ok, ActDismiss)
	k.SetMask(0, 0, 0, 1)
	a, ok = k.Press(keyG, time.Now())
	wantAction(t, a, ok, Insert("п"))
}

func TestAHeldKeyRepeatsUnderTheCurrentModifiers(t *testing.T) {
	k, t0 := keyboard(t), time.Now()
	k.Press(keyA, t0)
	if d, ok := k.Deadline(); !ok || !d.Equal(t0.Add(ms(300))) {
		t.Fatalf("deadline %v %v", d, ok)
	}
	k.Press(keyShiftL, t0)
	k.SetMask(mShift, 0, 0, 0)
	if _, ok := k.Deadline(); !ok {
		t.Fatal("a modifier stopped the repeat")
	}
	due := k.RepeatsDue(t0.Add(ms(340)))
	want := Repeated{Code: keyA, Action: Insert("A"), OK: true}
	if len(due) != 2 || due[0] != want || due[1] != want {
		t.Errorf("due: %+v", due)
	}
	k.Release(keyA)
	if _, ok := k.Deadline(); ok {
		t.Error("the released key repeats")
	}
}

func TestAKeymapChangeAndFocusLossStopRepeatAndCompose(t *testing.T) {
	k, t0 := keyboard(t), time.Now()
	k.Press(keyA, t0)
	k.SetKeymap(keymap(t, "us"))
	if _, ok := k.Deadline(); ok {
		t.Error("a repeat survived the keymap")
	}
	k.Press(keyA, t0)
	k.Press(keyCompose, t0)
	if _, ok := k.Deadline(); ok {
		t.Error("another key did not end the repeat")
	}
	k.Press(keyO, t0)
	k.FocusOut()
	k.FocusIn()
	a, ok := shifted(k, keyQuote, t0)
	wantAction(t, a, ok, Insert("\"")) // no stale sequence
}

func TestFocusLossDismissesByDefaultAndKeepsOnRequest(t *testing.T) {
	k := keyboard(t)
	a, ok := k.FocusOut()
	wantAction(t, a, ok, ActDismiss)
	k.SetFocusLoss(config.FocusKeep)
	a, ok = k.FocusOut()
	wantNothing(t, a, ok)
}

func TestRateZeroNeverSchedulesARepeat(t *testing.T) {
	k := keyboard(t)
	k.SetRepeatRate(0, 300)
	k.Press(keyA, time.Now())
	if _, ok := k.Deadline(); ok {
		t.Error("a deadline at rate 0")
	}
	if due := k.RepeatsDue(time.Now().Add(5 * time.Second)); len(due) != 0 {
		t.Errorf("due: %+v", due)
	}
}

func TestTheKeypadTypesDigitsOnlyWithNumLock(t *testing.T) {
	k, t0 := keyboard(t), time.Now()
	a, ok := k.Press(keyKP1, t0)
	wantAction(t, a, ok, EditAction(Edit{Kind: End}))
	k.SetMask(0, 0, mNum, 0)
	a, ok = k.Press(keyKP1, t0)
	wantAction(t, a, ok, Insert("1"))
}
