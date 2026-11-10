package dotenv

import (
	"strings"
	"testing"

	composedotenv "github.com/compose-spec/compose-go/v2/dotenv"
)

func TestEncodeValue_RoundTripsThroughComposeParser(t *testing.T) {
	values := []string{
		"plain",
		"with space",
		" leading and trailing ",
		"hash # sign",
		"#starts with hash",
		`quote " and ' marks`,
		"dollar $HOME and ${VAR} and $$",
		`back\slash and \n literal`,
		"trailing backslash\\",
		"multi\nline\r\nvalue",
		"tab\there",
		"=equals=",
		"ünïcödé ✓",
		"a'b",
		"'",
		`"`,
		"export",
	}
	for _, value := range values {
		doc := Parse("")
		doc.Set("KEY", value, true)

		parsed, err := composedotenv.UnmarshalWithLookup(doc.String(), nil)
		if err != nil {
			t.Fatalf("Compose failed to parse %q for value %q: %v", doc.String(), value, err)
		}
		if parsed["KEY"] != value {
			t.Fatalf("Compose read %q for value %q (written as %q)", parsed["KEY"], value, doc.String())
		}

		if got := entriesByKey(Parse(doc.String()))["KEY"].Value; got != value {
			t.Fatalf("editor read %q for value %q (written as %q)", got, value, doc.String())
		}
	}
}

func TestEncodeValue_EveryPlainCharacterReadsBackUnquoted(t *testing.T) {
	for _, r := range "_@%+:,./=-" {
		for _, value := range []string{"a" + string(r) + "b", string(r) + "b", "b" + string(r), string(r)} {
			if got := EncodeValue(value); got != value {
				t.Fatalf("expected %q to stay plain, got %q", value, got)
			}

			parsed, err := composedotenv.UnmarshalWithLookup("KEY="+value+"\n", nil)
			if err != nil {
				t.Fatalf("Compose failed to parse plain value %q: %v", value, err)
			}
			if parsed["KEY"] != value {
				t.Fatalf("Compose read %q for plain value %q", parsed["KEY"], value)
			}
		}
	}
}

func TestEncodeValue_ExactOutput(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"", ""},
		{"plain", "plain"},
		{"a b", `"a b"`},
		{`a"b`, `"a\"b"`},
		{"a'b", `"a'b"`},
		{`a\b`, `"a\\b"`},
		{"$HOME", `"\$HOME"`},
		{"#x", `"#x"`},
		{"l1\nl2", `"l1\nl2"`},
		{"l1\r\nl2", `"l1\r\nl2"`},
		{"a\tb", `"a\tb"`},
	}
	for _, tt := range tests {
		if got := EncodeValue(tt.value); got != tt.want {
			t.Fatalf("EncodeValue(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestEncodeValue_EscapeLookalikesStayLiteral(t *testing.T) {
	values := []string{
		" ",
		"\t",
		"$",
		"${",
		"${VAR:-default}",
		`\$`,
		`\\`,
		`\"`,
		`\'`,
		`\n`,
		`\t`,
		`\0`,
		`\012`,
		`\x41`,
		`é`,
		`ends with backslash \`,
		`"quoted"`,
		`'quoted'`,
		"# comment",
		"a #comment",
		"export A=1",
		strings.Repeat("long value ", 1500),
	}
	for _, value := range values {
		parsed, err := composedotenv.UnmarshalWithLookup("KEY="+EncodeValue(value)+"\nNEXT=1\n", nil)
		if err != nil {
			t.Fatalf("Compose failed to parse the encoded value %q: %v", value, err)
		}
		if parsed["KEY"] != value {
			t.Fatalf("Compose read %q for value %q", parsed["KEY"], value)
		}
		if parsed["NEXT"] != "1" {
			t.Fatalf("the following line was lost for value %q", value)
		}
	}
}

func TestParse_ReadsLikeCompose(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"plain", "K=v\n"},
		{"empty value", "K=\n"},
		{"no trailing newline", "K=v"},
		{"spaces around equals", "K = v\n"},
		{"leading spaces in value", "K=   v\n"},
		{"indented key", "  K=v\n"},
		{"export", "export K=v\n"},
		{"yaml style", "K: v\n"},
		{"yaml style without space", "K:v\n"},
		{"duplicate keys", "K=1\nK=2\n"},
		{"value with equals", "K=a=b\n"},
		{"value with colon", "K=http://h:1/p\n"},
		{"hash without space", "K=a#b\n"},
		{"comment right after equals", "K= # c\nL=1\n"},
		{"inline comment", "K=v # comment\n"},
		{"comment lines", "# c\nK=v\n   # indented\nL=w\n"},
		{"double quotes", "K=\"a b # c\"\n"},
		{"double quote escapes", "K=\"a\\nb\\tc\\\"d\\\\e\\$f\"\n"},
		{"single quotes", "K='a\\nb $x'\n"},
		{"comment after quote", "K=\"v\" # c\nL='w' # c\n"},
		{"multi-line value", "K=\"a\nb\"\nL=1\n"},
		{"crlf", "A=1\r\nB=\"x\r\ny\"\r\nC=3 # c\r\n"},
		{"bracket key", "list[0]=x\n"},
		{"key starting with a digit", "1ABC=x\n"},
		{"key starting with a dot or dash", ".A=1\n-B=2\n"},
		{"unicode key", "ключ=x\nklíč=y\n"},
		{"key without value", "K\nL=1\n"},
		{"blank lines", "\n\nK=v\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theirs, err := composedotenv.UnmarshalWithLookup(tt.text, nil)
			if err != nil {
				t.Fatalf("Compose failed to parse %q: %v", tt.text, err)
			}
			ours := make(map[string]string)
			for _, entry := range Parse(tt.text).Entries() {
				ours[entry.Key] = entry.Value
			}

			for key, want := range theirs {
				if got, ok := ours[key]; !ok || got != want {
					t.Errorf("key %q: Compose reads %q, the editor reads %q (found: %v) in %q", key, want, got, ok, tt.text)
				}
			}
			for key := range ours {
				if _, ok := theirs[key]; !ok {
					t.Errorf("the editor lists key %q, Compose does not have it in %q", key, tt.text)
				}
			}
		})
	}
}

func TestEdit_ResultIsVerifiedByCompose(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		changes []Change
	}{
		{"set new", "A=1\n", []Change{{Key: "B", Value: "two words"}}},
		{"set existing", "# c\nA=1 # note\nB=2\n", []Change{{Key: "A", Value: "new"}}},
		{"set duplicate", "A=1\nA=2\n", []Change{{Key: "A", Value: "3"}}},
		{"set multi-line", "A=\"x\ny\"\nB=1\n", []Change{{Key: "A", Value: "short"}}},
		{"set to multi-line", "A=1\n", []Change{{Key: "A", Value: "x\ny"}}},
		{"set with special characters", "A=1\n", []Change{{Key: "A", Value: "$HOME \"q\" \\ # \t"}}},
		{"set in crlf file", "A=1\r\nB=2\r\n", []Change{{Key: "C", Value: "3"}, {Key: "A", Value: "x y"}}},
		{"set in file without newline", "A=1", []Change{{Key: "B", Value: "2"}}},
		{"delete", "# orcacd:public\nA=1\nB=2\n", []Change{{Key: "A", Delete: true}}},
		{"delete duplicate", "A=1\nB=2\nA=3\n", []Change{{Key: "A", Delete: true}}},
		{"delete yaml style", "A: 1\nB=2\n", []Change{{Key: "A", Delete: true}}},
		{"set yaml style", "A: 1\nB=2\n", []Change{{Key: "A", Value: "9"}}},
		{"delete missing", "A=1\n", []Change{{Key: "B", Delete: true}}},
		{"mixed", "A=1\nB=2\nC=3\n", []Change{{Key: "A", Delete: true}, {Key: "B", Value: "x"}, {Key: "D", Value: "4"}}},
		{"empty file", "", []Change{{Key: "A", Value: "1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.text)
			for _, change := range tt.changes {
				if change.Delete {
					doc.Remove(change.Key)
				} else {
					doc.Set(change.Key, change.Value, true)
				}
			}
			if err := VerifyEdit(tt.text, doc.String(), tt.changes); err != nil {
				t.Fatalf("%v\nbefore: %q\nafter:  %q", err, tt.text, doc.String())
			}
		})
	}
}

func TestEncodeValue_PlainValuesAreNotQuoted(t *testing.T) {
	for _, value := range []string{"debug", "https://example.com:8080/a-b_c", "1.2.3", "a@b.c", "x=y", ""} {
		if got := EncodeValue(value); got != value {
			t.Fatalf("expected %q to stay plain, got %q", value, got)
		}
	}
	if got := EncodeValue("a b"); !strings.HasPrefix(got, `"`) {
		t.Fatalf("expected a value with a space to be quoted, got %q", got)
	}
}
