// Package native links the spec §4 libraries that no binding calls yet,
// through pkg-config: libfontconfig (D4) and libfreetype (D2). The others
// come with their bindings: libwayland-client (internal/wlproto),
// libwayland-egl and libEGL (internal/egl), libGLESv2 (internal/gles).
// Nothing else is linked: no libvulkan, libxkbcommon or libharfbuzz.
//
// The package has no Go API. It pins the link line until the font
// bindings call these libraries themselves; then it goes, with its blank
// import in cmd/logis. Every C object a binding creates is owned by one
// locked goroutine and released explicitly, never through finalizers.
package native

/*
#cgo pkg-config: fontconfig freetype2
*/
import "C"
