package config

import (
	"errors"
	"path/filepath"
	"testing"
)

// custom-shader (a5e5f876): the custom pass's shader
// file, a path from the config's directory; --check-config compiles it.

func TestACustomShaderIsAPathFromTheConfigsDirectory(t *testing.T) {
	if c := parse(t, `{"custom-shader": "shaders/glow.frag"}`); c.CustomShader != "shaders/glow.frag" || c.Equal(Default()) {
		t.Errorf("%+v", c)
	}
	if c := parse(t, `{"custom-shader": ""}`); !c.Equal(Default()) {
		t.Errorf("an empty path is not none: %+v", c)
	}
	if e := refused(t, `{"custom-shader": 3}`); e.Message != "invalid type: integer `3`, expected "+
		"`custom-shader` as the path of a GLSL ES 3.00 fragment shader file" {
		t.Errorf("%s", e.Message)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "logis.jsonc")
	for text, want := range map[string]string{
		`{"custom-shader": "shaders/glow.frag"}`: filepath.Join(dir, "shaders", "glow.frag"),
		`{"custom-shader": "/opt/glow.frag"}`:    "/opt/glow.frag",
		`{}`:                                     "",
	} {
		write(t, path, text)
		if c, err := NewSource(path).Load(); err != nil || c.CustomShader != want {
			t.Errorf("%s: %q %v", text, c.CustomShader, err)
		}
	}
}

func TestCheckCompilesTheCustomShader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, `{"custom-shader": "glow.frag"}`)
	var asked Config
	verdict, err := Check(NewSource(path), soundTheme, func(dir string, c Config) (string, error) {
		asked = c
		return c.CustomShader, nil
	})
	glow := filepath.Join(filepath.Dir(path), "glow.frag")
	want := "the config at " + path + " is sound; the theme 'forge-crimson-brass' is sound; " +
		"the custom shader " + glow + " is sound"
	if err != nil || verdict != want || asked.CustomShader != glow {
		t.Errorf("%q %v %+v", verdict, err, asked)
	}
	_, err = Check(NewSource(path), soundTheme, func(string, Config) (string, error) {
		return glow, errors.New(glow + ":3:33: error: `uw' undeclared")
	})
	if want := "the custom shader is heretical: " + glow + ":3:33: error: `uw' undeclared"; err == nil || err.Error() != want {
		t.Errorf("%v", err)
	}
}
