// One entry point per spec §4 library that no binding calls yet,
// referenced so that the linker records each library even with
// --as-needed. Nothing here is called. libwayland-client and
// libwayland-egl (internal/wlproto, internal/egl), libEGL (internal/egl)
// and libGLESv2 (internal/gles) are linked by their bindings.

#include <fontconfig/fontconfig.h>
#include <ft2build.h>
#include FT_FREETYPE_H

void *const logis_native_entry_points[] = {
	(void *)FcInitLoadConfig,      // libfontconfig
	(void *)FT_Init_FreeType,      // libfreetype
};
