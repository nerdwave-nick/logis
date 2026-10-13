package main

import (
	"bufio"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/nth"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Ported from Rust src/client/dmenu/mod.rs and stream.rs.

func chosen(index uint64, raw string) protocol.Outcome {
	return protocol.Outcome{Kind: protocol.Selected, Index: index, Item: protocol.Item{Raw: []byte(raw)}}
}

func mustNth(t *testing.T, text string) *nth.Nth {
	t.Helper()
	n, err := nth.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func prints(t *testing.T, o protocol.Outcome, d dmenuOptions, want string) {
	t.Helper()
	got, ok := choice(o, d)
	if !ok || string(got) != want {
		t.Fatalf("%q (%v), want %q", got, ok, want)
	}
}

func TestAChosenItemPrintsItsOriginalBytes(t *testing.T) {
	prints(t, chosen(3, "a\tb \xff"), defaultDmenu(), "a\tb \xff")
}

func TestIndexPrintsThePositionInTheInput(t *testing.T) {
	d := defaultDmenu()
	d.index = true
	prints(t, chosen(41, "x"), d, "41")
}

func TestAcceptNthPrintsTheChosenColumns(t *testing.T) {
	d := defaultDmenu()
	d.columns.Accept, d.columns.Delimiter = mustNth(t, "2"), ','
	prints(t, chosen(0, "1,First"), d, "First")
}

func TestTypedTextPrintsAsTypedWhateverTheOutputRunes(t *testing.T) {
	d := defaultDmenu()
	d.index, d.columns.Accept = true, mustNth(t, "2")
	prints(t, protocol.Outcome{Kind: protocol.Typed, Text: "free\ttext"}, d, "free\ttext")
}

func TestEveryOtherEndingPrintsNothing(t *testing.T) {
	for _, k := range []protocol.OutcomeKind{protocol.Dismissed, protocol.Replaced, protocol.Cancelled, protocol.Closed} {
		if got, ok := choice(protocol.Outcome{Kind: k}, defaultDmenu()); ok {
			t.Fatalf("%s printed %q", k, got)
		}
	}
}

// chunks hands out its chunks one Read at a time.
type chunks [][]byte

func (c *chunks) Read(p []byte) (int, error) {
	if len(*c) == 0 {
		return 0, io.EOF
	}
	n := copy(p, (*c)[0])
	*c = (*c)[1:]
	return n, nil
}

// frames are the frames streamed from these chunks, each as its items' raw
// bytes, END for the end.
func frames(t *testing.T, separator byte, in ...string) [][]string {
	t.Helper()
	return framesBuffered(t, 64<<10, separator, in...)
}

// framesBuffered is frames read through a buffer of size bytes.
func framesBuffered(t *testing.T, size int, separator byte, in ...string) [][]string {
	t.Helper()
	c := chunks{}
	for _, s := range in {
		c = append(c, []byte(s))
	}
	var sent [][]string
	streamItems(bufio.NewReaderSize(&c, size), separator,
		func(raw []byte) protocol.Item { return protocol.Item{Raw: raw} },
		func(r protocol.Request) bool {
			switch r.Type {
			case protocol.TypeItems:
				var raws []string
				for _, i := range r.Items {
					raws = append(raws, string(i.Raw))
				}
				sent = append(sent, raws)
			case protocol.TypeEnd:
				sent = append(sent, []string{"END"})
			default:
				t.Fatalf("unexpected %+v", r)
			}
			return true
		})
	return sent
}

func sameFrames(t *testing.T, got, want [][]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestLinesAreSentAsTheyArriveThenEnd(t *testing.T) {
	sameFrames(t, frames(t, '\n', "a\nb\n", "c\n"), [][]string{{"a", "b"}, {"c"}, {"END"}})
}

func TestOriginalBytesAreKeptAndAFinalLineNeedsNoNewline(t *testing.T) {
	sameFrames(t, frames(t, '\n', "\xff\r\n\n", "tail"), [][]string{{"\xff\r", ""}, {"tail"}, {"END"}})
}

func TestEmptyInputSendsOnlyEnd(t *testing.T) {
	sameFrames(t, frames(t, '\n'), [][]string{{"END"}})
}

func TestALargeInputIsSplitIntoBoundedBatches(t *testing.T) {
	sent := frames(t, '\n', strings.Repeat("x\n", batchItems+1))
	var sizes []int
	for _, f := range sent {
		sizes = append(sizes, len(f))
	}
	if !reflect.DeepEqual(sizes, []int{batchItems, 1, 1}) {
		t.Fatalf("%v", sizes)
	}
}

// With more buffered than a batch may carry, the byte bound ends it.
func TestABatchStaysUnderItsByteBound(t *testing.T) {
	line := strings.Repeat("y", 64<<10) + "\n"
	sent := framesBuffered(t, 1<<20, '\n', strings.Repeat(line, 6))
	if len(sent[0]) != 4 {
		t.Fatalf("first batch %d lines, want 4 (256 KiB)", len(sent[0]))
	}
}

func TestStreamingStopsWhenTheDaemonIsGone(t *testing.T) {
	c := chunks{[]byte("a\n"), []byte("b\n")}
	calls := 0
	streamItems(bufio.NewReader(&c), '\n', func(raw []byte) protocol.Item { return protocol.Item{Raw: raw} },
		func(protocol.Request) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("%d sends", calls)
	}
}

func TestItemsDisplayTheirLossyText(t *testing.T) {
	i := dmenuItem([]byte("caf\xc3\xa9 \xff"), defaultDmenu().columns)
	if i.Text != "café �" || i.Match != nil || string(i.Raw) != "caf\xc3\xa9 \xff" {
		t.Fatalf("%+v", i)
	}
}

func TestNulSeparatedInputKeepsNewlinesInsideItems(t *testing.T) {
	sameFrames(t, frames(t, 0, "a\nb\x00c\x00", "d"), [][]string{{"a\nb", "c"}, {"d"}, {"END"}})
}

func TestWithNthShapesTheTextAndMatchNthTheMatchText(t *testing.T) {
	cols := nth.Columns{With: mustNth(t, "2"), Match: mustNth(t, "{3} {1}"), Delimiter: '\t'}
	i := dmenuItem([]byte("id\tShown \xff\tsecret"), cols)
	if i.Text != "Shown �" || i.Match == nil || *i.Match != "secret id" || string(i.Raw) != "id\tShown \xff\tsecret" {
		t.Fatalf("%+v", i)
	}
}
