package apps

import (
	"errors"
	"fmt"
	"strings"
)

// One desktop entry of Type=Application, read from its key file: what the
// launcher shows, matches, hides and runs (Desktop Entry Specification,
// "Recognized desktop entry keys" and "Additional applications actions").
//
//   - Name (required), GenericName and Keywords are localised;
//   - Exec is required unless DBusActivatable=true; an invalid one makes
//     the entry invalid;
//   - Hidden=true means the entry was deleted: nothing else is required;
//   - actions named in Actions need their [Desktop Action id] group with a
//     Name; an action without one, or with an invalid Exec, is skipped.

// DesktopEntry is a desktop entry of an application.
type DesktopEntry struct {
	// Name is the translated Name.
	Name string
	// GenericName is the translated GenericName ("Web Browser").
	GenericName string
	// Keywords are the translated Keywords.
	Keywords []string
	// Icon is a themed icon name or an absolute path.
	Icon string
	// Exec is nil only for a D-Bus activatable or hidden entry.
	Exec *Exec
	// TryExec is a program that must exist for the entry to show.
	TryExec string
	// Path is the working directory.
	Path string
	// Terminal: run in a terminal.
	Terminal bool
	// NoDisplay: never shown in menus.
	NoDisplay bool
	// Hidden: deleted.
	Hidden bool
	// OnlyShowIn are the desktops it shows in (all when empty).
	OnlyShowIn []string
	// NotShowIn are the desktops it never shows in.
	NotShowIn []string
	// DBusActivatable: started through D-Bus (org.freedesktop.Application).
	DBusActivatable bool
	// Actions are the desktop actions, in Actions order.
	Actions []Action
}

// Action is an additional entry point of an application.
type Action struct {
	// ID is its identifier (in Actions and [Desktop Action id]).
	ID string
	// Name is its translated Name.
	Name string
	// Icon is its own Icon, if it has one.
	Icon string
	// Exec is nil only for a D-Bus activatable application.
	Exec *Exec
}

var (
	// ErrNoDesktopEntry: the file has no [Desktop Entry] group.
	ErrNoDesktopEntry = errors.New("no [Desktop Entry] group")
	// ErrNoName: the entry has no Name.
	ErrNoName = errors.New("no Name")
	// ErrNoExec: no Exec, and not D-Bus activatable.
	ErrNoExec = errors.New("no Exec and not DBusActivatable")
)

// NotAnApplicationError: Type is not Application (a link or directory, or
// missing).
type NotAnApplicationError struct{ Type string }

func (e *NotAnApplicationError) Error() string {
	return fmt.Sprintf("Type=%s, not Application", e.Type)
}

// ParseEntry reads an application's desktop entry, localised for l.
func ParseEntry(text string, l Locale) (DesktopEntry, error) {
	f, err := ParseKeyFile(text)
	if err != nil {
		return DesktopEntry{}, fmt.Errorf("malformed: %w", err)
	}
	g, ok := f.Group("Desktop Entry")
	if !ok {
		return DesktopEntry{}, ErrNoDesktopEntry
	}
	if g.Bool("Hidden") {
		return DesktopEntry{Hidden: true}, nil
	}
	if kind, _ := g.String("Type"); kind != "Application" {
		return DesktopEntry{}, &NotAnApplicationError{Type: kind}
	}
	name, _ := g.Localized("Name", l)
	if name == "" {
		return DesktopEntry{}, ErrNoName
	}
	dbus := g.Bool("DBusActivatable")
	exec, err := execOf(g)
	if err != nil {
		return DesktopEntry{}, err
	}
	if exec == nil && !dbus {
		return DesktopEntry{}, ErrNoExec
	}
	e := DesktopEntry{
		Name:            name,
		Keywords:        g.LocalizedList("Keywords", l),
		Exec:            exec,
		Terminal:        g.Bool("Terminal"),
		NoDisplay:       g.Bool("NoDisplay"),
		OnlyShowIn:      g.List("OnlyShowIn"),
		NotShowIn:       g.List("NotShowIn"),
		DBusActivatable: dbus,
	}
	e.GenericName, _ = g.Localized("GenericName", l)
	e.Icon, _ = g.Localized("Icon", l)
	e.TryExec, _ = g.String("TryExec")
	e.Path, _ = g.String("Path")
	for _, id := range g.List("Actions") {
		if a, ok := action(f, id, l, dbus); ok {
			e.Actions = append(e.Actions, a)
		}
	}
	return e, nil
}

// action is action id of f, if it is complete and sound.
func action(f *KeyFile, id string, l Locale, dbus bool) (Action, bool) {
	g, ok := f.Group("Desktop Action " + id)
	if !ok {
		return Action{}, false
	}
	name, _ := g.Localized("Name", l)
	exec, err := execOf(g)
	if name == "" || err != nil || exec == nil && !dbus {
		return Action{}, false
	}
	icon, _ := g.Localized("Icon", l)
	return Action{ID: id, Name: name, Icon: icon, Exec: exec}, true
}

// execOf is a group's Exec, nil if it has none.
func execOf(g *Group) (*Exec, error) {
	value, _ := g.String("Exec")
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	e, err := ParseExec(value)
	if err != nil {
		return nil, err
	}
	return &e, nil
}
