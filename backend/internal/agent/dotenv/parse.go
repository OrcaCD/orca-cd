package dotenv

import (
	"regexp"
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

	for i := 0; i < len(lines); {
		item, consumed := scanItem(lines, i)
		doc.items = append(doc.items, item)
		i += consumed
	}
	return doc
}

func scanItem(lines []string, i int) (item, int) {
	line := lines[i]
	opaque := item{raw: []string{line}}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return opaque, 1
	}
	if strings.HasPrefix(trimmed, "#") {
		opaque.marker = strings.TrimSpace(trimmed[1:]) == PublicMarker
		return opaque, 1
	}

	statement := exportPattern.ReplaceAllString(strings.TrimLeft(line, " \t"), "")
	// Compose also accepts "KEY: value" and splits at whichever comes first.
	idx := strings.IndexAny(statement, "=:")
	if idx < 0 {
		return opaque, 1
	}
	key := strings.TrimSpace(statement[:idx])
	if !envrules.KeyPattern.MatchString(key) {
		return opaque, 1
	}

	rest := strings.TrimLeft(statement[idx+1:], " \t")
	if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
		value, consumed, ok := scanQuotedValue(lines, i, rest)
		if !ok {
			return opaque, 1
		}
		return item{raw: lines[i : i+consumed], key: key, value: value}, consumed
	}

	value, _, _ := strings.Cut(rest, " #")
	value = strings.TrimRightFunc(value, unicode.IsSpace)
	return item{raw: []string{line}, key: key, value: value}, 1
}

// scanQuotedValue mirrors the Compose parser, including values that continue
// on the following lines. rest starts with the opening quote on
// lines[startLine].
func scanQuotedValue(lines []string, startLine int, rest string) (string, int, bool) {
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

			value := string(chars)
			if quote == '"' {
				value = unescape(value)
			}
			return value, line - startLine + 1, true
		}
	}
	return "", 0, false
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
