package e2e

import "time"

// promptNS: a compositor answering round trips within this has finished
// starting up.
const promptNS = 5_000_000

// waitForAnswers waits until the compositor answers two round trips in a
// row within promptNS. A nested niri creates its socket before it has
// finished starting: measured over 10 starts, its first round trip took
// 24–67 ms and its second 11–24 ms (load 3–4), only later ones under 5 ms.
// A test or measurement must not take that start-up for its own (the
// watchdog saw it as a stall before the one under test).
func (c *Compositor) waitForAnswers() {
	c.t.Helper()
	d, err := ConnectVerified(c.socket, c.PID())
	if err != nil {
		c.t.Fatal(err)
	}
	defer d.Close()
	deadline := time.Now().Add(Patience)
	for prompt := 0; prompt < 2; {
		if time.Now().After(deadline) {
			c.t.Fatalf("the %s test compositor never answered within %v in %v:\n%s",
				c.kind.Name(), time.Duration(promptNS), Patience, c.Log())
		}
		asked := time.Now()
		if err := d.Roundtrip(); err != nil {
			c.t.Fatalf("the %s test compositor does not answer: %v\n%s", c.kind.Name(), err, c.Log())
		}
		if time.Since(asked) < promptNS {
			prompt++
		} else {
			prompt = 0
		}
		time.Sleep(watchEvery)
	}
}
