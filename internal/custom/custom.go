// Package custom is the custom post-processing pass's shader (feat/logis-go
// a5e5f876): one GLSL ES 3.00 fragment shader a theme
// (effects.custom-shader) or the config (custom-shader) names, run after
// the built-in effects. It is written as for Shadertoy: it defines
// mainImage(out vec4 fragColor, in vec2 fragCoord), and logis gives it the
// frame after the built-in effects (iChannel0, rows bottom-up as
// fragCoord's), iResolution, iTime, iFrame and iGlitch in a prelude of its
// own (Fragment). What the driver says of a shader it refuses is moved to
// the user's lines (Explain). The renderer compiles it; this package draws
// on no GL.
package custom

import (
	"os"
	"path/filepath"

	"github.com/nerdwave-nick/logis/internal/oserr"
)

// Shader is a custom pass's shader as read from its file.
type Shader struct {
	// Path is the file, absolute; "" is no custom pass.
	Path string
	// Source is its text; Problem why it could not be read ("" when it
	// was).
	Source, Problem string
	// Frozen: motion is reduced, iTime stands at 0 (the pass still runs).
	Frozen bool
}

// Set reports whether a custom pass is asked for.
func (s Shader) Set() bool { return s.Path != "" }

// Load reads the shader at path (absolute); "" is none. A file that cannot
// be read leaves a shader with the Problem, which the renderer reports
// once and skips.
func Load(path string) Shader {
	if path == "" {
		return Shader{}
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return Shader{Path: path, Problem: oserr.Words(unwrapPath(err))}
	}
	return Shader{Path: path, Source: string(text)}
}

func unwrapPath(err error) error {
	if p, ok := err.(*os.PathError); ok {
		return p.Err
	}
	return err
}

// Readable is why the shader could not be read, as an *Error; nil when it
// was.
func (s Shader) Readable() error {
	if s.Problem == "" {
		return nil
	}
	return &Error{Path: s.Path, Problems: []Problem{{Text: s.Problem}}}
}

// Beside is path as a file in dir names it: relative paths start in dir
// (the theme's or the config's directory); "" stays "".
func Beside(dir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// Heresy is how a refused custom shader is reported on the command line
// and in the log.
func Heresy(err error) string { return "the custom shader is heretical: " + err.Error() }
