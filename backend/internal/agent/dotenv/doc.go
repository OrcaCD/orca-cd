// Package dotenv edits dotenv files one assignment at a time and leaves
// everything else, such as comments, ordering and quoting of untouched lines,
// as it was. A key is a variable if the line above it is the comment
// "# orcacd:public", otherwise it is a secret. The syntax follows the Compose
// dotenv parser.
package dotenv

import (
	"slices"
	"strings"
)

// PublicMarker is the comment above a key that marks it as a variable. Keys
// without it are secrets, so existing env files are safe by default.
const PublicMarker = "orcacd:public"

type item struct {
	markers []string
	raw     []string
	key     string
	value   string
	prefix  string
	suffix  string
	// markerLine is set on a marker line. Those directly above an assignment
	// move into its markers, the others stay as items of their own.
	markerLine bool
	// joined is set on a statement that continues the line of the previous item.
	joined bool
}

// public is true if the assignment has a marker directly above it.
func (i item) public() bool {
	return len(i.markers) > 0
}

type Doc struct {
	items   []item
	bom     bool
	crlf    bool
	finalNL bool
}

// byteOrderMark is what some editors put at the start of a file. Compose skips
// it, so the parser does too and writes it back.
var byteOrderMark = string(rune(0xFEFF))

type Entry struct {
	Key    string
	Value  string
	Public bool
}

func (d *Doc) String() string {
	var b strings.Builder
	if d.bom {
		b.WriteString(byteOrderMark)
	}
	lines := 0
	writeLine := func(line string) {
		if lines > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		lines++
	}
	for _, item := range d.items {
		for _, marker := range item.markers {
			writeLine(marker)
		}
		raw := item.raw
		if item.joined {
			b.WriteString(raw[0])
			raw = raw[1:]
		}
		for _, line := range raw {
			writeLine(line)
		}
	}
	if lines > 0 && d.finalNL {
		b.WriteByte('\n')
	}
	return b.String()
}

func (d *Doc) lastIndex(key string) int {
	for i, item := range slices.Backward(d.items) {
		if item.key == key {
			return i
		}
	}
	return -1
}

// Entries returns each key once, as it is seen by Compose.
func (d *Doc) Entries() []Entry {
	last := make(map[string]int, len(d.items))
	for i, item := range d.items {
		if item.key != "" {
			last[item.key] = i
		}
	}

	entries := make([]Entry, 0, len(last))
	for i, item := range d.items {
		if item.key == "" || last[item.key] != i {
			continue
		}
		entries = append(entries, Entry{Key: item.key, Value: item.value, Public: item.public()})
	}
	return entries
}

func (d *Doc) KeyCount() int {
	seen := make(map[string]struct{})
	for _, item := range d.items {
		if item.key != "" {
			seen[item.key] = struct{}{}
		}
	}
	return len(seen)
}

func (d *Doc) newLine(text string) string {
	if d.crlf {
		return text + "\r"
	}
	return text
}

func (d *Doc) markerLines() []string {
	return []string{d.newLine("# " + PublicMarker)}
}

func (d *Doc) assignment(key, value string) item {
	prefix := key + "="
	return item{
		raw:    []string{d.render(prefix, value, "", false)},
		key:    key,
		value:  value,
		prefix: prefix,
	}
}

func (d *Doc) render(prefix, value, suffix string, inline bool) string {
	encoded := EncodeValue(value)
	// Compose reads "K= # c" as the value "# c", and an unquoted value would
	// swallow the statement after it.
	if inline || (encoded == "" && suffix != "") {
		encoded = quoteValue(value)
	}
	if inline {
		return prefix + encoded + suffix
	}
	return d.newLine(prefix + encoded + suffix)
}

// Set creates or updates key. The line is only rewritten if the value changes,
// so a flag change keeps values such as ${VAR} as they are.
func (d *Doc) Set(key, value string, public bool) {
	idx := d.lastIndex(key)
	if idx < 0 {
		added := d.assignment(key, value)
		switch {
		case public:
			added.markers = d.markerLines()
		case len(d.items) > 0 && d.items[len(d.items)-1].markerLine:
			// Without a blank line the stray marker at the end of the file
			// would make the new secret public.
			d.items = append(d.items, item{raw: []string{d.newLine("")}})
		}
		d.items = append(d.items, added)
		d.finalNL = true
		return
	}

	existing := &d.items[idx]
	if existing.value != value {
		inline := idx+1 < len(d.items) && d.items[idx+1].joined
		existing.raw = []string{d.render(existing.prefix, value, existing.suffix, inline)}
		existing.value = value
	}

	switch {
	case public && !existing.public():
		existing.markers = d.markerLines()
		if existing.joined {
			existing.joined = false
			previous := &d.items[idx-1]
			previous.raw[len(previous.raw)-1] += d.newLine("")
		}
	case !public:
		existing.markers = nil
	}
}

func (d *Doc) Remove(key string) {
	kept := make([]item, 0, len(d.items))
	lineStartRemoved := false
	for i, item := range d.items {
		if item.key == key {
			endsLine := i+1 == len(d.items) || !d.items[i+1].joined
			if item.joined && endsLine && !lineStartRemoved {
				previous := &kept[len(kept)-1]
				previous.raw[len(previous.raw)-1] += d.newLine("")
			}
			lineStartRemoved = lineStartRemoved || !item.joined
			continue
		}
		if item.joined && lineStartRemoved {
			item.joined = false
		}
		lineStartRemoved = false
		kept = append(kept, item)
	}
	d.items = kept
}
