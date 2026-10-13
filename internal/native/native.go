// Package native links the C libraries spec §4 allows in the shipped binary,
// through pkg-config: libwayland-client and libwayland-egl (D5, and the EGL
// Wayland platform of D1), libEGL and libGLESv2 (D1), libfontconfig (D4) and
// libfreetype (D2). Nothing else is linked: no libvulkan, libxkbcommon or
// libharfbuzz.
//
// The package has no Go API. It pins the link line until the bindings of
// later tickets (Wayland, renderer, fonts) call these libraries themselves;
// those packages declare their own `#cgo pkg-config` lines. Every C object
// they create is owned by one locked goroutine and released explicitly,
// never through finalizers.
package native

/*
#cgo pkg-config: wayland-client wayland-egl egl glesv2 fontconfig freetype2
*/
import "C"
