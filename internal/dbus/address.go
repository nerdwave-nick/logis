package dbus

import (
	"strconv"
	"strings"
)

// A Socket is where a socket address points: a path (unix:path=…) or an
// abstract name (unix:abstract=…).
type Socket struct {
	Name     string
	Abstract bool
}

// SessionAddress is the session bus's address in an environment (getenv
// says "" for unset); "" if none.
func SessionAddress(getenv func(string) string) string {
	if a := getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" {
		return a
	}
	if d := getenv("XDG_RUNTIME_DIR"); strings.HasPrefix(d, "/") {
		return "unix:path=" + d + "/bus"
	}
	return ""
}

// Sockets are the Unix sockets an address names, in order (other transports
// skipped).
func Sockets(address string) []Socket {
	var sockets []Socket
	for _, one := range strings.Split(address, ";") {
		keys, ok := strings.CutPrefix(one, "unix:")
		if !ok {
			continue
		}
		for _, pair := range strings.Split(keys, ",") {
			key, value, ok := strings.Cut(pair, "=")
			if ok && (key == "path" || key == "abstract") {
				sockets = append(sockets, Socket{unescape(value), key == "abstract"})
				break
			}
		}
	}
	return sockets
}

// unescape undoes an address value's %XX escapes.
func unescape(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) && isHex(value[i+1]) && isHex(value[i+2]) {
			n, _ := strconv.ParseUint(value[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
			continue
		}
		b.WriteByte(value[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// dialName is the name unixsock dials for a socket.
func (s Socket) dialName() string {
	if s.Abstract {
		// Package syscall reads a leading @ as Linux's abstract namespace.
		return "@" + s.Name
	}
	return s.Name
}
