package wlproto

// Per protocol: the client header and the interface tables from
// wayland-scanner, then the Go wrappers from wlgen. The core protocol
// (wayland.xml) has only the Go wrappers: libwayland-client carries its
// header and tables.

//go:generate go run ../wlgen/cmd/wlgen protocols/wayland.xml wayland_gen.go

//go:generate wayland-scanner client-header protocols/cursor-shape-v1.xml cursor-shape-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/cursor-shape-v1.xml cursor-shape-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/cursor-shape-v1.xml cursor_shape_v1_gen.go

//go:generate wayland-scanner client-header protocols/ext-background-effect-v1.xml ext-background-effect-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/ext-background-effect-v1.xml ext-background-effect-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/ext-background-effect-v1.xml ext_background_effect_v1_gen.go

//go:generate wayland-scanner client-header protocols/fractional-scale-v1.xml fractional-scale-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/fractional-scale-v1.xml fractional-scale-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/fractional-scale-v1.xml fractional_scale_v1_gen.go

//go:generate wayland-scanner client-header protocols/presentation-time.xml presentation-time-client-protocol.h
//go:generate wayland-scanner private-code protocols/presentation-time.xml presentation-time-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/presentation-time.xml presentation_time_gen.go

//go:generate wayland-scanner client-header protocols/primary-selection-unstable-v1.xml primary-selection-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/primary-selection-unstable-v1.xml primary-selection-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/primary-selection-unstable-v1.xml primary_selection_unstable_v1_gen.go

//go:generate wayland-scanner client-header protocols/tablet-v2.xml tablet-v2-client-protocol.h
//go:generate wayland-scanner private-code protocols/tablet-v2.xml tablet-v2-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/tablet-v2.xml tablet_v2_gen.go

//go:generate wayland-scanner client-header protocols/viewporter.xml viewporter-client-protocol.h
//go:generate wayland-scanner private-code protocols/viewporter.xml viewporter-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/viewporter.xml viewporter_gen.go

//go:generate wayland-scanner client-header protocols/virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/virtual-keyboard-unstable-v1.xml virtual-keyboard-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/virtual-keyboard-unstable-v1.xml virtual_keyboard_unstable_v1_gen.go

//go:generate wayland-scanner client-header protocols/wlr-layer-shell-unstable-v1.xml wlr-layer-shell-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/wlr-layer-shell-unstable-v1.xml wlr-layer-shell-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/wlr-layer-shell-unstable-v1.xml wlr_layer_shell_unstable_v1_gen.go

//go:generate wayland-scanner client-header protocols/xdg-activation-v1.xml xdg-activation-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/xdg-activation-v1.xml xdg-activation-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/xdg-activation-v1.xml xdg_activation_v1_gen.go

//go:generate wayland-scanner client-header protocols/xdg-shell.xml xdg-shell-client-protocol.h
//go:generate wayland-scanner private-code protocols/xdg-shell.xml xdg-shell-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/xdg-shell.xml xdg_shell_gen.go

//go:generate wayland-scanner client-header protocols/wlr-data-control-unstable-v1.xml wlr-data-control-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/wlr-data-control-unstable-v1.xml wlr-data-control-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/wlr-data-control-unstable-v1.xml wlr_data_control_unstable_v1_gen.go

//go:generate wayland-scanner client-header protocols/wlr-screencopy-unstable-v1.xml wlr-screencopy-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/wlr-screencopy-unstable-v1.xml wlr-screencopy-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/wlr-screencopy-unstable-v1.xml wlr_screencopy_unstable_v1_gen.go

//go:generate wayland-scanner client-header protocols/wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-client-protocol.h
//go:generate wayland-scanner private-code protocols/wlr-virtual-pointer-unstable-v1.xml wlr-virtual-pointer-unstable-v1-protocol.c
//go:generate go run ../wlgen/cmd/wlgen protocols/wlr-virtual-pointer-unstable-v1.xml wlr_virtual_pointer_unstable_v1_gen.go
