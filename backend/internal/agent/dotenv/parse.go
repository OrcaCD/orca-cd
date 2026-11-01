package dotenv

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
)

var (
	exportPattern = regexp.MustCompile(`^export\s+`)
	// escapePattern matches the escape sequences Compose decodes in double
	// quoted values. Keep it identical to escapeSeqRegex in
	// https://github.com/compose-spec/compose-go/blob/v2.16.1/dotenv/parser.go#L19
	escapePattern = regexp.MustCompile(`\\(?:[abcfnrtv$"\\]|0\d{0,3})`)
)

// Parse reads the text of a dotenv file. It never fails: lines it does not
// understand are kept as they are.
func Parse(text string) *Doc {
	doc := &Doc{}
	if strings.HasPrefix(text, byteOrderMark) {
		doc.bom = true
		text = text[len(byteOrderMark):]
	}
	doc.crlf = strings.Contains(text, "\r\n")
	if text == "" {
		return doc
	}

	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		doc.finalNL = true
		lines = lines[:len(lines)-1]
	}

	joined := false
	for i := 0; i < len(lines); {
		item, consumed, tail := scanItem(lines, i)
		item.joined = joined
		if item.key != "" && !joined {
			doc.attachMarkers(&item)
		}
		doc.items = append(doc.items, item)

		joined = tail != ""
		i += consumed
		if joined {
			i--
			lines[i] = tail
		}
	}
	return doc
}

func (d *Doc) attachMarkers(assignment *item) {
	start := len(d.items)
	for start > 0 && d.items[start-1].markerLine {
		start--
	}
	for _, marker := range d.items[start:] {
		assignment.markers = append(assignment.markers, marker.raw...)
	}
	d.items = d.items[:start]
}

// scanItem returns the item starting at lines[i], the number of lines it uses
// and, if another statement follows the closing quote, the text of it.
func scanItem(lines []string, i int) (item, int, string) {
	line := lines[i]
	opaque := item{raw: []string{line}}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return opaque, 1, ""
	}
	if strings.HasPrefix(trimmed, "#") {
		opaque.markerLine = strings.TrimSpace(trimmed[1:]) == PublicMarker
		return opaque, 1, ""
	}

	// Slice instead of ReplaceAllString, which copies the rest of the line for
	// every statement on it.
	statement := strings.TrimLeft(line, " \t")
	if loc := exportPattern.FindStringIndex(statement); loc != nil {
		statement = statement[loc[1]:]
	}
	// Compose also accepts "KEY: value" and splits at whichever comes first.
	idx := strings.IndexAny(statement, "=:")
	if idx < 0 {
		return opaque, 1, ""
	}
	key := strings.TrimSpace(statement[:idx])
	if !envrules.KeyPattern.MatchString(key) {
		return opaque, 1, ""
	}

	rest := strings.TrimLeftFunc(statement[idx+1:], isValueSpace)
	prefix := line[:len(line)-len(rest)]
	if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
		value, consumed, tail, ok := scanQuotedValue(lines, i, rest)
		if !ok {
			return opaque, 1, ""
		}
		quoted := item{
			raw:    slices.Clone(lines[i : i+consumed]),
			key:    key,
			value:  value,
			prefix: prefix,
		}
		if t := strings.TrimSpace(tail); t != "" && t[0] != '#' {
			last := len(quoted.raw) - 1
			quoted.raw[last] = quoted.raw[last][:len(quoted.raw[last])-len(tail)]
			return quoted, consumed, tail
		}
		quoted.suffix = commentSuffix(tail)
		return quoted, consumed, ""
	}

	value, _, _ := strings.Cut(rest, " #")
	value = strings.TrimRightFunc(value, unicode.IsSpace)
	return item{
		raw:    []string{line},
		key:    key,
		value:  value,
		prefix: prefix,
		suffix: commentSuffix(rest[len(value):]),
	}, 1, ""
}

// isValueSpace matches isSpace in the Compose parser, except for "\r", which is
// the line ending of CRLF files here.
func isValueSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\v', '\f', 0x85, 0xA0:
		return true
	}
	return false
}

func commentSuffix(tail string) string {
	tail = strings.TrimRight(tail, "\r")
	if !strings.HasPrefix(strings.TrimSpace(tail), "#") {
		return ""
	}
	if strings.HasPrefix(tail, "#") {
		return " " + tail
	}
	return tail
}

// scanQuotedValue mirrors the Compose parser, including values that continue
// on the following lines. rest starts with the opening quote on
// lines[startLine]. tail is the text after the closing quote.
func scanQuotedValue(lines []string, startLine int, rest string) (value string, consumed int, tail string, ok bool) {
	quote := rest[0]
	var chars []byte
	escaped := false

	for line := startLine; line < len(lines); line++ {
		segment := lines[line]
		if line == startLine {
			segment = rest[1:]
		}
		for pos := 0; pos <= len(segment); pos++ {
			// The newline between two lines is part of the value.
			var char byte = '\n'
			if pos < len(segment) {
				char = segment[pos]
			} else if line == len(lines)-1 {
				break
			}

			if char != quote {
				if !escaped && char == '\\' {
					escaped = true
					continue
				}
				if escaped {
					escaped = false
					chars = append(chars, '\\')
				}
				chars = append(chars, char)
				continue
			}
			if escaped {
				escaped = false
				chars = append(chars, char)
				continue
			}

			value = string(chars)
			if quote == '"' {
				value = unescape(value)
			}
			return value, line - startLine + 1, segment[pos+1:], true
		}
	}
	return "", 0, "", false
}

func unescape(s string) string {
	return escapePattern.ReplaceAllStringFunc(s, func(match string) string {
		if match == `\$` {
			return "$"
		}
		if strings.HasPrefix(match, `\0`) {
			match = strings.Replace(match, `\0`, `\`, 1)
		}
		decoded, _, _, err := strconv.UnquoteChar(match, '"')
		if err != nil {
			return match
		}
		return string(decoded)
	})
}
