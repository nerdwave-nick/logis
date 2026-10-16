package text

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/fc"
)

// ConfigError: fontconfig could not load a configuration (Rust
// FontError::Config).
type ConfigError struct{ Why string }

func (e *ConfigError) Error() string { return "the font litanies are corrupt: " + e.Why }

// NoFaceError: no face answers for the family (FontError::NoFace).
type NoFaceError struct{ Family string }

func (e *NoFaceError) Error() string {
	return fmt.Sprintf("no font answers the rite: fontconfig found no face for '%s'", e.Family)
}

// UnreadableError: the resolved file could not be read as a font
// (FontError::Unreadable).
type UnreadableError struct{ File fc.FontFile }

func (e *UnreadableError) Error() string {
	return fmt.Sprintf("the font scroll %s (face %d) is unreadable", e.File.Path, e.File.Index)
}

// System is the user's own fontconfig setup, aliases included.
func System() (*fc.Config, error) {
	c, err := fc.System()
	if err != nil {
		return nil, &ConfigError{Why: err.Error()}
	}
	return c, nil
}

// Isolated is a configuration with only the rules in xml (a <fontconfig>
// document, for aliases) and only the given font files, nothing from the
// system: pinned fonts for tests and goldens.
func Isolated(xml string, files []string) (*fc.Config, error) {
	c, err := fc.Isolated(xml, files)
	if err != nil {
		return nil, &ConfigError{Why: err.Error()}
	}
	return c, nil
}
