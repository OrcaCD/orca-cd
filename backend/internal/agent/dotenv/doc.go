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
	raw    []string
	key    string
	value  string
	marker bool
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
	var lines []string
	for _, item := range d.items {
		lines = append(lines, item.raw...)
	}
	text := strings.Join(lines, "\n")
	if len(lines) > 0 && d.finalNL {
		text += "\n"
	}
	if d.bom {
		text = byteOrderMark + text
	}
	return text
}

func (d *Doc) isPublic(idx int) bool {
	return idx > 0 && d.items[idx-1].marker
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
	entries := make([]Entry, 0, len(d.items))
	for i, item := range d.items {
		if item.key == "" || d.lastIndex(item.key) != i {
			continue
		}
		entries = append(entries, Entry{Key: item.key, Value: item.value, Public: d.isPublic(i)})
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

func (d *Doc) markerItem() item {
	return item{raw: []string{d.newLine("# " + PublicMarker)}, marker: true}
}

func (d *Doc) assignment(key, value string) item {
	return item{
		raw:   []string{d.newLine(key + "=" + EncodeValue(value))},
		key:   key,
		value: value,
	}
}

// Set creates or updates key. A line that already has the requested value and
// flag is left as it is.
func (d *Doc) Set(key, value string, public bool) {
	idx := d.lastIndex(key)
	if idx < 0 {
		if public {
			d.items = append(d.items, d.markerItem())
		}
		d.items = append(d.items, d.assignment(key, value))
		d.finalNL = true
		return
	}

	if d.items[idx].value == value && d.isPublic(idx) == public {
		return
	}
	d.items[idx] = d.assignment(key, value)

	switch hasMarker := d.isPublic(idx); {
	case public && !hasMarker:
		d.items = append(d.items[:idx], append([]item{d.markerItem()}, d.items[idx:]...)...)
	case !public && hasMarker:
		d.items = append(d.items[:idx-1], d.items[idx:]...)
	}
}

func (d *Doc) Remove(key string) {
	kept := make([]item, 0, len(d.items))
	for i, item := range d.items {
		switch {
		case item.key == key:
		case item.marker && i+1 < len(d.items) && d.items[i+1].key == key:
		default:
			kept = append(kept, item)
		}
	}
	d.items = kept
}
