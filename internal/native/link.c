// One entry point per library spec §4 allows, referenced so that the linker
// records each library even with --as-needed. Nothing here is called.

#include <EGL/egl.h>
#include <GLES3/gl3.h>
#include <fontconfig/fontconfig.h>
#include <ft2build.h>
#include FT_FREETYPE_H
#include <wayland-client.h>
#include <wayland-egl.h>

void *const logis_native_entry_points[] = {
	(void *)wl_display_connect,    // libwayland-client
	(void *)wl_egl_window_create,  // libwayland-egl
	(void *)eglGetDisplay,         // libEGL
	(void *)glGetString,           // libGLESv2
	(void *)FcInitLoadConfig,      // libfontconfig
	(void *)FT_Init_FreeType,      // libfreetype
};
