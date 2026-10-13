// The hand-written C side of package wlproto: one dispatcher for every
// proxy's events (into Go), request marshalling, and the poll the event
// loop waits in.
#ifndef LOGIS_WLPROTO_GLUE_H
#define LOGIS_WLPROTO_GLUE_H

#include <poll.h>
#include <stdint.h>
#include <wayland-client.h>

// Tags the proxy with its Go handle and routes its events to Go.
int logis_wl_listen(struct wl_proxy *proxy, uintptr_t handle);

// The Go handle of a proxy (0 for none or NULL).
uintptr_t logis_wl_handle(void *proxy);

// Sends a request; returns the new proxy for a constructor.
struct wl_proxy *logis_wl_marshal(struct wl_proxy *proxy, uint32_t opcode,
	const struct wl_interface *iface, uint32_t version, uint32_t flags,
	union wl_argument *args);

// Waits until one of the n fds has its events, or timeout_ms passes (-1:
// no timeout): the display's fd, the wake eventfd and the watched fds.
// Returns poll's result.
int logis_wl_poll(struct pollfd *fds, int n, int timeout_ms);

// A non-blocking, close-on-exec eventfd.
int logis_wl_eventfd(void);

#endif
