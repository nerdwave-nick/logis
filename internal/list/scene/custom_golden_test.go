package scene_test

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/custom"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// A custom pass (a5e5f876) over each bundled theme's prompt-only panel:
// testdata/warm-rows.frag, written for this golden, tints the frame
// after the built-in effects and dims every fourth buffer row.
func TestGoldenACustomShaderAfterTheBuiltInEffects(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "warm-rows.frag"))
	if err != nil {
		t.Fatal(err)
	}
	shader := custom.Load(path)
	if shader.Problem != "" {
		t.Fatal(shader.Problem)
	}
	o := list.DefaultOptions()
	o.Prompt, o.Lines, o.Width, o.Password = "pin: ", u32(0), u32(40), '*'
	v := list.NewView(o)
	v.Apply(input.Insert("1234"))
	withShader := func(_ *render.Headless, _ scale.Scale, l *list.Look, logical scale.Size, st scene.Style) *render.Scene {
		th := *st.Theme
		th.Custom = shader
		st.Theme = &th
		return scene.Draw(l, logical, st)
	}
	checkThemes(t, "custom", v, o, nil, withShader)
}
