package text

import (
	"runtime"

	"github.com/nerdwave-nick/logis/internal/fc"
)

// PendingFonts are fonts being resolved on a goroutine of their own, so
// that fontconfig's start-up overlaps the compositor's and the GPU's
// (Rust PendingFonts, ticket 9d8ec26d): loading its configuration and
// caches, matching the family against every installed face and sorting
// the fallback order each cost milliseconds per thousand faces, and none
// needs the GPU.
type PendingFonts struct{ done chan resolved }

type resolved struct {
	fonts *Fonts
	err   error
}

// Resolve starts resolving family from the configuration config makes:
// the default face is loaded and the fallback order sorted, so the first
// text only loads the fallback faces it needs.
func Resolve(config func() (*fc.Config, error), family string) *PendingFonts {
	return start(config, family, nil)
}

// ResolveSystem resolves the user's fontconfig and its DefaultFamily.
func ResolveSystem() *PendingFonts { return start(System, DefaultFamily, nil) }

// ResolveSystemWith resolves the user's fontconfig with faces as the faces
// texts name by index (a theme's). A face that cannot be resolved is the
// default family; that is no error here, the theme still shows.
func ResolveSystemWith(faces []Face) *PendingFonts {
	return start(System, DefaultFamily, faces)
}

func start(config func() (*fc.Config, error), family string, faces []Face) *PendingFonts {
	p := &PendingFonts{done: make(chan resolved, 1)}
	go func() {
		// The C objects are made on one locked thread; the Fonts then
		// moves to its owner through the channel, never shared.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			if recover() != nil {
				p.done <- resolved{err: &ConfigError{Why: "the font thread perished"}}
			}
		}()
		c, err := config()
		if err != nil {
			p.done <- resolved{err: err}
			return
		}
		fonts, err := New(c, family)
		if err != nil {
			p.done <- resolved{err: err}
			return
		}
		if faces != nil {
			_ = fonts.SetFaces(faces)
		}
		fonts.sortFallbacks()
		p.done <- resolved{fonts: fonts}
	}()
	return p
}

// Wait is the resolved fonts once they are ready, for the caller to own
// and Close. Call it once. A resolver that panicked is an error, not a
// panic of the caller.
func (p *PendingFonts) Wait() (*Fonts, error) {
	r := <-p.done
	return r.fonts, r.err
}
