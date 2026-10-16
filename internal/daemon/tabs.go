package daemon

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nerdwave-nick/logis/internal/provider"
)

// Plain logis's tabbed launcher in the daemon (feat/logis-go 8cba2fc9, new
// beside Rust v1): plain logis with servitor on its PATH opens an app
// session that names its rites, servitor mode's provider. The session is
// shown as two tabs of one window, the applications and the rites,
// opening on the tab last used; the provider starts with the session, and
// its first stage fills the rites' tab instead of showing the window.
// Every step of the provider is for that tab, and each answer goes to the
// feed of the tab it comes from. The tab of a tabbed session's answer
// (a dismissal included) is remembered in $XDG_STATE_HOME/logis/tab, beside
// frecency; --apps and --servitor neither read nor change it.

// tabWords are the remembered tabs' words in the file, by Tab.
var tabWords = []string{"applications", "rites"}

// tabPath is where the tab last used is kept beside the frecency file
// ("" when that is kept nowhere).
func tabPath(frecencyPath string) string {
	if frecencyPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(frecencyPath), "tab")
}

// loadTab is the tab kept at path: the applications when there is none,
// or the file says something else.
func loadTab(path string) provider.Tab {
	b, err := os.ReadFile(path)
	if err != nil {
		return provider.TabApps
	}
	for i, w := range tabWords {
		if strings.TrimSpace(string(b)) == w {
			return provider.Tab(i)
		}
	}
	return provider.TabApps
}

// saveTab keeps tab at path, replacing the file whole.
func saveTab(path string, tab provider.Tab) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(tabWords[tab]+"\n"), 0o666); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return errors.Join(err, rmErr)
		}
		return err
	}
	return nil
}

// rememberTab notes the tab a tabbed session was answered from.
func (c *core) rememberTab(tab provider.Tab) {
	if int(tab) >= len(tabWords) || tab == c.apps.tab {
		return
	}
	c.apps.tab = tab
	if c.apps.tabPath == "" {
		return
	}
	if err := saveTab(c.apps.tabPath, tab); err != nil {
		c.logf("the tab last used is not kept in " + c.apps.tabPath + ": " + err.Error())
	}
}

// answeredProvider: the user answered the rites' tab of tabbed session id
// (or the only list of a provider session) through its provider l.
func (c *core) answeredProvider(id SessionID, l *live, answer Answer) {
	if l == nil {
		return
	}
	if err := l.external.Answer(answer); err != nil {
		c.fail(id, "the provider was lost: "+err.Error())
	}
}
