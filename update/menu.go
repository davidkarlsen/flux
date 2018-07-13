package update

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/pkg/term"

	"github.com/weaveworks/flux"
)

// Escape sequences.
const clearLines = "\033[%dA"
const hideCursor = "\033[?25l"
const showCursor = "\033[?25h"

type WriteFlusher interface {
	io.Writer
	Flush() error
}

type ClearableWriter struct {
	wf    WriteFlusher
	lines int
}

func NewClearableWriter(wf WriteFlusher) *ClearableWriter {
	return &ClearableWriter{wf, 0}
}

func (c *ClearableWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		if b == '\n' {
			c.lines++
		}
	}
	return c.wf.Write(p)
}

func (c *ClearableWriter) Clear() {
	fmt.Fprintf(c.wf, clearLines, c.lines)
	c.lines = 0
}

func (c *ClearableWriter) Flush() error {
	return c.wf.Flush()
}

type MenuItem struct {
	id       flux.ResourceID
	status   ControllerUpdateStatus
	error    string
	update   *ContainerUpdate
	selected bool
}

type Menu struct {
	out        *ClearableWriter
	results    Result
	items      []MenuItem
	cursor     int
	selectable int
}

// PrintResults outputs a result set to the `io.Writer` provided, at
// the given level of verbosity:
//  - 2 = include skipped and ignored resources
//  - 1 = include skipped resources, exclude ignored resources
//  - 0 = exclude skipped and ignored resources
func NewMenu(out io.Writer, results Result, verbosity int) *Menu {
	m := &Menu{
		out:     NewClearableWriter(tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)),
		results: results,
	}
	m.fromResults(results, verbosity)
	return m
}

func (m *Menu) fromResults(results Result, verbosity int) {
	for _, serviceID := range results.ServiceIDs() {
		resourceID := flux.MustParseResourceID(serviceID)
		result := results[resourceID]
		switch result.Status {
		case ReleaseStatusIgnored:
			if verbosity < 2 {
				continue
			}
		case ReleaseStatusSkipped:
			if verbosity < 1 {
				continue
			}
		}

		if result.Error != "" {
			m.AddItem(MenuItem{
				id:     resourceID,
				status: result.Status,
				error:  result.Error,
			})
		}
		for _, upd := range result.PerContainer {
			m.AddItem(MenuItem{
				id:     resourceID,
				status: result.Status,
				update: &upd,
			})
		}
	}
}

func (m *Menu) AddItem(mi MenuItem) {
	m.items = append(m.items, mi)
	if mi.selectable() {
		m.selectable++
	}
}

func (m *Menu) toggleCursor() {
	m.items[m.cursor].selected = !m.items[m.cursor].selected
	m.Render()
}

func (m *Menu) cursorDown() {
	m.cursor = (m.cursor + 1) % m.selectable
	m.Render()
}

func (m *Menu) cursorUp() {
	m.cursor = (m.cursor + m.selectable - 1) % m.selectable
	m.Render()
}

func (m *Menu) Run() (selected []ContainerUpdate, aborted bool) {
	defer fmt.Printf(showCursor)

	m.Render()
	for {
		ascii, keyCode, err := getChar()

		if (ascii == 3 || ascii == 27) || err != nil {
			fmt.Fprintln(m.out, "Aborted.")
			return selected, true
		}

		if ascii == ' ' {
			m.toggleCursor()
		} else if ascii == 13 {
			for _, item := range m.items {
				if item.selected {
					selected = append(selected, *item.update)
				}
			}
			fmt.Println()
			return
		}

		if keyCode == 40 {
			m.cursorDown()
		} else if keyCode == 38 {
			m.cursorUp()
		}
	}
	return
}

func (m *Menu) Render() {
	m.out.Clear()

	fmt.Fprintln(m.out, "   CONTROLLER \tSTATUS \tUPDATES")
	i := 0
	for _, item := range m.items {
		m.renderItem(&item, i == m.cursor)
		if item.selectable() {
			i++
		}
	}
	fmt.Printf(hideCursor)
	m.out.Flush()
}

func (m *Menu) renderItem(item *MenuItem, cursor bool) {
	curs := " "
	if cursor {
		curs = ">"
	}
	fmt.Fprintf(m.out, "%s%s %s\t%s\t%s\n", curs, item.checkbox(), item.id.String(), item.status, item.updates())
}

func (i MenuItem) selectable() bool {
	return i.update != nil
}

func (i MenuItem) checkbox() string {
	switch {
	case !i.selectable():
		return " "
	case i.selected:
		return "\u25c9"
	default:
		return "\u25ef"
	}
}

func (i MenuItem) updates() string {
	if i.update != nil {
		return fmt.Sprintf("%s: %s -> %s",
			i.update.Container,
			i.update.Current.String(),
			i.update.Target.Tag)
	}
	return i.error
}

// See https://github.com/paulrademacher/climenu/blob/master/getchar.go
func getChar() (ascii int, keyCode int, err error) {
	t, _ := term.Open("/dev/tty")
	term.RawMode(t)
	bytes := make([]byte, 3)

	var numRead int
	numRead, err = t.Read(bytes)
	if err != nil {
		return
	}
	if numRead == 3 && bytes[0] == 27 && bytes[1] == 91 {
		// Three-character control sequence, beginning with "ESC-[".

		// Since there are no ASCII codes for arrow keys, we use
		// Javascript key codes.
		if bytes[2] == 65 {
			// Up
			keyCode = 38
		} else if bytes[2] == 66 {
			// Down
			keyCode = 40
		} else if bytes[2] == 67 {
			// Right
			keyCode = 39
		} else if bytes[2] == 68 {
			// Left
			keyCode = 37
		}
	} else if numRead == 1 {
		ascii = int(bytes[0])
	} else {
		// Two characters read??
	}
	t.Restore()
	t.Close()
	return
}
