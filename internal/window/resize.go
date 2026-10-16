package window

import "github.com/nerdwave-nick/logis/internal/scale"

// Resize asks for the visible panel at size logical pixels (a theme of
// another size worn while it shows). The request is double-buffered: it
// goes out with the next frame's commit, and the compositor's configure
// brings the new size; a configure that leaves a side to the client takes
// this one. Without a visible panel it does nothing.
func (w *Window) Resize(size scale.Size) {
	if w.panel == nil {
		return
	}
	w.panel.layer.SetSize(size.W, size.H)
	w.panel.state.request(size)
}
