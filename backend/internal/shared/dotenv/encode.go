package dotenv

import "strings"

// EncodeValue returns value in a form the Compose parser reads back
// unchanged: plain if that is safe, otherwise double quoted.
func EncodeValue(value string) string {
	if value == "" {
		return ""
	}
	if isPlain(value) {
		return value
	}
	return quoteValue(value)
}

func quoteValue(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func isPlain(value string) bool {
	for _, r := range value {
		if !isPlainRune(r) {
			return false
		}
	}
	return true
}

func isPlainRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		strings.ContainsRune("_@%+:,./=-", r)
}
