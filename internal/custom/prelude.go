package custom

import (
	"regexp"
	"strconv"
	"strings"
)

// prelude comes before the user's source: the version, default
// precisions and the inputs logis gives, Shadertoy's names (and iGlitch,
// logis's own). The frame is premultiplied; fragColor is taken as
// premultiplied too.
const prelude = `#version 300 es
precision highp float;
precision highp int;
uniform highp sampler2D iChannel0;
uniform vec3 iResolution;
uniform float iTime;
uniform int iFrame;
uniform float iGlitch;
out vec4 logis_fragColor;
`

// PreludeLines is how many lines come before the user's first.
var PreludeLines = strings.Count(prelude, "\n")

// mainWords is how the explanation of a refusal in logis's main names it.
const mainWords = "logis's main, which calls mainImage(out vec4 fragColor, in vec2 fragCoord)"

// epilogue comes after the user's source: main, which calls mainImage at
// the pixel's centre (bottom-up, as gl_FragCoord is).
const epilogue = `
void main() {
	vec4 color = vec4(0.0);
	mainImage(color, gl_FragCoord.xy);
	logis_fragColor = color;
}
`

var versionLine = regexp.MustCompile(`^\s*#\s*version\b(.*)$`)

// Fragment is the fragment shader logis compiles for source (the file at
// path): the prelude, the source with its own `#version 300 es` (if any)
// blanked, so its lines keep their numbers, and main. Any other version is
// refused at its line.
func Fragment(path, source string) (string, error) {
	lines := strings.Split(source, "\n")
	for i, l := range lines {
		m := versionLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if strings.Join(strings.Fields(m[1]), " ") != "300 es" {
			return "", &Error{Path: path, Problems: []Problem{{
				Line: i + 1,
				Text: "`" + strings.TrimSpace(l) + "`: logis speaks GLSL ES 3.00 alone (#version 300 es, which it gives itself)",
			}}}
		}
		lines[i] = ""
	}
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return prelude + body + epilogue, nil
}

// Error is a custom shader logis or the driver refused, each problem at
// its place in the user's file.
type Error struct {
	Path     string
	Problems []Problem
}

// MaxProblems is how many problems an Error says; the rest are counted
// (a driver may list every candidate of a call it could not match).
const MaxProblems = 6

// Problem is one thing wrong with a shader.
type Problem struct {
	// Line and Column are in the user's file, from 1; 0 is unknown.
	Line, Column int
	Text         string
}

func (e *Error) Error() string {
	var b strings.Builder
	for i, p := range e.Problems {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i == MaxProblems {
			b.WriteString(e.Path + ": … and " + strconv.Itoa(len(e.Problems)-i) + " more")
			break
		}
		b.WriteString(e.Path)
		if p.Line > 0 {
			b.WriteString(":" + strconv.Itoa(p.Line))
			if p.Column > 0 {
				b.WriteString(":" + strconv.Itoa(p.Column))
			}
		}
		b.WriteString(": " + p.Text)
	}
	return b.String()
}

// The places drivers give in their logs: Mesa's "0:12(5): error: …",
// the "ERROR: 0:12: …" of ANGLE, glslang and most mobile drivers, and
// NVIDIA's "0(12) : error C0000: …".
var (
	mesaPlace   = regexp.MustCompile(`^\d+:(\d+)\((\d+)\):\s*(.*)$`)
	anglePlace  = regexp.MustCompile(`^(ERROR|WARNING):\s*\d+:(\d+):\s*(.*)$`)
	nvidiaPlace = regexp.MustCompile(`^\d+\((\d+)\)\s*:\s*(.*)$`)
)

// Explain is the driver's log for a refused Fragment of source (the file
// at path) as an *Error in the user's lines: a place in the prelude or in
// main is said in words, a line without a place is kept as it is.
func Explain(path, source, log string) error {
	user := strings.Count(strings.TrimSuffix(source, "\n"), "\n") + 1
	e := &Error{Path: path}
	for l := range strings.SplitSeq(log, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		e.Problems = append(e.Problems, place(l, user))
	}
	if len(e.Problems) == 0 {
		e.Problems = []Problem{{Text: "the machine spirit refused it without a word"}}
	}
	return e
}

// place is one line of a driver's log at its place in a user's source of
// user lines.
func place(l string, user int) Problem {
	var line, column int
	var text string
	if m := mesaPlace.FindStringSubmatch(l); m != nil {
		line, _ = strconv.Atoi(m[1])
		column, _ = strconv.Atoi(m[2])
		text = m[3]
	} else if m := anglePlace.FindStringSubmatch(l); m != nil {
		line, _ = strconv.Atoi(m[2])
		text = strings.ToLower(m[1]) + ": " + m[3]
	} else if m := nvidiaPlace.FindStringSubmatch(l); m != nil {
		line, _ = strconv.Atoi(m[1])
		text = m[2]
	} else {
		return Problem{Text: l}
	}
	switch at := line - PreludeLines; {
	case at < 1:
		return Problem{Text: text + " (in logis's prelude)"}
	case at > user:
		return Problem{Text: text + " (in " + mainWords + ")"}
	default:
		return Problem{Line: at, Column: column, Text: text}
	}
}
