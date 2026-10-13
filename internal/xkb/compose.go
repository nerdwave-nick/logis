package xkb

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Compose tables (XCompose(5)) as libxkbcommon reads them: the same file
// lookup (compose_paths.go), the same handling of overriding sequences,
// modifiers in sequences parsed and ignored, at most 10 keysyms per
// sequence, more than 10 bad lines failing the file.

const (
	composeMaxLHS     = 10
	composeMaxInclude = 5
	composeMaxErrors  = 10
	composeMaxString  = 256
	// composeMaxBytes bounds what one table reads, includes and all (the
	// largest system table, en_US.UTF-8, is about 600 KB).
	composeMaxBytes = 32 << 20
)

type composeNode struct {
	next    map[Keysym]*composeNode
	leaf    bool
	hasText bool
	text    string
	keysym  Keysym
}

// ComposeTable is a parsed Compose table. It is immutable once loaded.
type ComposeTable struct {
	root    *composeNode
	locale  string
	env     ComposeEnv
	read    int    // bytes read so far, includes too
	Path    string // the file it was loaded from ("" from a string)
	Skipped int    // lines skipped as libxkbcommon skips them
}

// ComposeFromString parses a table from text; includes resolve against
// env and locale.
func ComposeFromString(env ComposeEnv, text, locale string) (*ComposeTable, error) {
	t := &ComposeTable{root: &composeNode{}, locale: locale, env: env}
	if err := t.parse(text, "<string>", 0); err != nil {
		return nil, err
	}
	return t, nil
}

// readFile reads a table or an include within composeMaxBytes in all.
func (t *ComposeTable) readFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	left := composeMaxBytes - t.read
	data, err := io.ReadAll(io.LimitReader(f, int64(left)+1))
	if err != nil {
		return "", err
	}
	if len(data) > left {
		return "", fmt.Errorf("%s: Compose tables of more than %d bytes in all", path, composeMaxBytes)
	}
	t.read += len(data)
	return string(data), nil
}

// Len is the number of sequences.
func (t *ComposeTable) Len() int { return t.root.countLeaves() }

func (n *composeNode) countLeaves() int {
	c := 0
	for _, ch := range n.next {
		if ch.leaf {
			c++
		}
		c += ch.countLeaves()
	}
	return c
}

func (t *ComposeTable) parse(text, path string, depth int) error {
	errs := 0
	for n, line := range strings.Split(text, "\n") {
		err := t.line(line, depth)
		if errors.Is(err, errSkip) {
			t.Skipped++
			continue
		}
		if err != nil {
			errs++
			if errs > composeMaxErrors {
				return fmt.Errorf("%s:%d: too many errors (last: %w)", path, n+1, err)
			}
		}
	}
	return nil
}

func (t *ComposeTable) include(p string, depth int) error {
	if depth+1 >= composeMaxInclude {
		return fmt.Errorf("includes nested too deeply")
	}
	path, err := t.expand(p)
	if err != nil {
		return err
	}
	data, err := t.readFile(path)
	if err != nil {
		return err
	}
	return t.parse(data, path, depth+1)
}

// expand resolves an include path's %H (home), %L (the locale's system
// table) and %S (the system Compose directory).
func (t *ComposeTable) expand(p string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '%' || i+1 >= len(p) {
			b.WriteByte(p[i])
			continue
		}
		i++
		switch p[i] {
		case '%':
			b.WriteByte('%')
		case 'H':
			if t.env.Home == "" {
				return "", fmt.Errorf("%%H used but HOME is not set")
			}
			b.WriteString(t.env.Home)
		case 'L':
			lp := localeComposePath(t.env, t.locale)
			if lp == "" {
				return "", fmt.Errorf("%%L used but the locale has no table")
			}
			b.WriteString(lp)
		case 'S':
			b.WriteString(t.env.XLocaleDir)
		default:
			return "", fmt.Errorf("unknown %% format %q in an include", p[i])
		}
	}
	return b.String(), nil
}

// add inserts a sequence; a later sequence overrides an equal one, any
// sequence it is a prefix of, and any that is a prefix of it.
func (t *ComposeTable) add(lhs []Keysym, text string, hasText bool, sym Keysym) {
	n := t.root
	for i, ks := range lhs {
		if n.next == nil {
			n.next = map[Keysym]*composeNode{}
		}
		child := n.next[ks]
		if child == nil {
			child = &composeNode{}
			n.next[ks] = child
		}
		if i < len(lhs)-1 {
			if child.leaf { // an existing sequence is a prefix: it goes
				child.leaf, child.hasText, child.text, child.keysym = false, false, "", NoSymbol
			}
			n = child
			continue
		}
		child.next = nil // a prefix of longer sequences: they go
		child.leaf, child.hasText, child.text, child.keysym = true, hasText, text, sym
	}
}
