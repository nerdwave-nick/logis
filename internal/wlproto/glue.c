#include <poll.h>
#include <sys/eventfd.h>

#include "glue.h"
#include "_cgo_export.h"

// Every event of every Go-owned proxy comes here; its argument count is
// the number of type letters in the message signature (digits give the
// version it appeared in, '?' marks a nullable argument).
static int logis_wl_dispatch(const void *data, void *target, uint32_t opcode,
	const struct wl_message *msg, union wl_argument *args) {
	(void)data;
	int n = 0;
	for (const char *s = msg->signature; *s; s++) {
		if (*s != '?' && (*s < '0' || *s > '9'))
			n++;
	}
	logisWlDispatch((uintptr_t)wl_proxy_get_user_data(target), opcode, args, n);
	return 0;
}

int logis_wl_listen(struct wl_proxy *proxy, uintptr_t handle) {
	wl_proxy_set_user_data(proxy, (void *)handle);
	return wl_proxy_add_dispatcher(proxy, logis_wl_dispatch, NULL, (void *)handle);
}

uintptr_t logis_wl_handle(void *proxy) {
	if (!proxy)
		return 0;
	return (uintptr_t)wl_proxy_get_user_data(proxy);
}

struct wl_proxy *logis_wl_marshal(struct wl_proxy *proxy, uint32_t opcode,
	const struct wl_interface *iface, uint32_t version, uint32_t flags,
	union wl_argument *args) {
	return wl_proxy_marshal_array_flags(proxy, opcode, iface, version, flags, args);
}

int logis_wl_poll(int fd, short events, int wake, int timeout_ms,
	short *revents, short *wake_revents) {
	struct pollfd fds[2] = {
		{ .fd = fd, .events = events },
		{ .fd = wake, .events = POLLIN },
	};
	int rc = poll(fds, 2, timeout_ms);
	*revents = fds[0].revents;
	*wake_revents = fds[1].revents;
	return rc;
}

int logis_wl_eventfd(void) {
	return eventfd(0, EFD_CLOEXEC | EFD_NONBLOCK);
}
