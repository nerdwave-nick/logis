package window

// Why the cogitator cannot be shown, in its own words. Every one of them
// ends logis with exit status 2.

// NoSessionError: no compositor could be reached or greeted.
type NoSessionError struct{ Reason string }

func (e *NoSessionError) Error() string {
	return "the noosphere is silent: no Wayland session answers (" + e.Reason +
		"). The cogitator manifests only within a Wayland session."
}

// NoLayerShellError: the compositor answered without wlr-layer-shell.
type NoLayerShellError struct{}

func (e *NoLayerShellError) Error() string {
	return "this compositor offers no altar: it does not support wlr-layer-shell (" + layerShell +
		"). The cogitator rises only on compositors that do, such as niri or sway; " +
		"GNOME/Mutter is not one of them."
}

// MissingGlobalError: a global the window needs is not offered.
type MissingGlobalError struct{ Interface string }

func (e *MissingGlobalError) Error() string {
	return "this compositor withholds a sacred rite: it does not offer " + e.Interface
}

// DisconnectedError: the compositor went away or broke the connection.
type DisconnectedError struct{ Reason string }

func (e *DisconnectedError) Error() string {
	return "the noosphere fell silent mid-rite: " + e.Reason
}

// SurfaceError: a frame could not be presented.
type SurfaceError struct{ Reason string }

func (e *SurfaceError) Error() string {
	return "the cogitator's glass would not take the image: " + e.Reason
}
