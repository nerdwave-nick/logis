package wlproto

// Per protocol: the client header, then the interface tables.

//go:generate wayland-scanner client-header protocols/viewporter.xml viewporter-client-protocol.h
//go:generate wayland-scanner private-code protocols/viewporter.xml viewporter-protocol.c
