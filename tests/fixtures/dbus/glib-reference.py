#!/usr/bin/env python3
"""Reference D-Bus messages encoded by GLib (GDBusMessage), an implementation
independent of logis's own (src/launch/wire.rs). Regenerate with
    python3 tests/fixtures/dbus/glib-reference.py > tests/fixtures/dbus/glib-reference.hex
One message per line, hex: StartTransientUnit, ActivateAction, Hello (calls,
little-endian), a Get reply carrying an `as` (big-endian) and an error reply."""
import gi
gi.require_version('Gio', '2.0')
from gi.repository import Gio, GLib


def blob(message, order=Gio.DBusMessageByteOrder.LITTLE_ENDIAN):
    message.set_byte_order(order)
    return bytes(message.to_blob(Gio.DBusCapabilityFlags.NONE)).hex()


def call(dest, path, iface, member, body, serial):
    m = Gio.DBusMessage.new_method_call(dest, path, iface, member)
    if body is not None:
        m.set_body(body)
    m.set_serial(serial)
    return m


print(blob(call('org.freedesktop.systemd1', '/org/freedesktop/systemd1',
                'org.freedesktop.systemd1.Manager', 'StartTransientUnit',
                GLib.Variant('(ssa(sv)a(sa(sv)))', (
                    'app-logis-forge\\x2danoint-4242.scope', 'fail',
                    [('PIDs', GLib.Variant('au', [4242])),
                     ('Slice', GLib.Variant('s', 'app.slice')),
                     ('Description', GLib.Variant('s', 'Anoint'))], [])), 7)))
print(blob(call('org.forge.Bus', '/org/forge/Bus', 'org.freedesktop.Application',
                'ActivateAction',
                GLib.Variant('(sava{sv})', ('wake', [], {
                    'activation-token': GLib.Variant('s', 'tok'),
                    'desktop-startup-id': GLib.Variant('s', 'tok')})), 3)))
print(blob(call('org.freedesktop.DBus', '/org/freedesktop/DBus',
                'org.freedesktop.DBus', 'Hello', None, 1)))
get = call('org.freedesktop.systemd1', '/org/freedesktop/systemd1',
           'org.freedesktop.DBus.Properties', 'Get', None, 9)
reply = Gio.DBusMessage.new_method_reply(get)
reply.set_body(GLib.Variant('(v)', (GLib.Variant(
    'as', ['PATH=/usr/bin', 'WAYLAND_DISPLAY=wayland-1', 'LANG=de_DE.UTF-8']),)))
reply.set_serial(31)
print(blob(reply, Gio.DBusMessageByteOrder.BIG_ENDIAN))
error = Gio.DBusMessage.new_method_error_literal(
    get, 'org.freedesktop.DBus.Error.ServiceUnknown', 'The name is not activatable')
error.set_serial(32)
print(blob(error))
