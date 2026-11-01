package dotenv

import (
	"maps"
	"strings"
	"testing"

	composedotenv "github.com/compose-spec/compose-go/v2/dotenv"
)

var composeCorpus = []string{
	"A=1\n",
	"A=1",
	"A=1\nB=2\nA=3\n",
	"A: 1\n",
	"A:1\nB=2\n",
	"export A=1\n",
	"export\tA=1\n",
	"  A=1\n",
	"A = 1\n",
	"A=1 # c\n",
	"A=1  # c\n",
	"A=a#b\n",
	"A=\n",
	"A= # c\n",
	"A=\"\"\n",
	"A=''\n",
	"A=\"x y\"\n",
	"A='x y'\n",
	"A=\"a\\nb\"\n",
	"A='a\\nb'\n",
	"A=\"a\\\"b\"\n",
	"A='a\\'b'\n",
	"A=\"x\\\\y\"\n",
	"A=\"\\t\\a\\0101\"\n",
	"A=\"x\ny\"\nB=2\n",
	"A='x\ny'\nB=2\n",
	"A=\"x\" # c\n",
	"A=\"x\"# c\n",
	"A=\"x\" B=2\n",
	"A=\"x\"B=2\n",
	"A='x' B=\"y z\" C=3\n",
	"A=\"x\ny\" B=2\n",
	"A=\"x\" junk\n",
	"A=\u00a0\"x\"\n",
	"A=\u00a0x\n",
	"A=\v1\n",
	"A=\f\"x\"\n",
	"A=\u0085'x'\n",
	"A=h\u00e9llo\n",
	"\u00e4=1\n",
	"a.b-c[1]=1\n",
	"# c\nA=1\n# d\n\nB=2\n",
	"# orcacd:public\nA=1\n# orcacd:public\n# orcacd:public\nB=2\n",
	"A=1\r\nB=\"x\"\r\n",
	"A=\"x\" B=2\r\nC=3\r\n",
	"A=1\r\n\r\nB=2\r\n",
	"A=1\nbare\nB=2\n",
	"# only a comment\n",
	"",
}

func composeVars(t *testing.T, text string) map[string]string {
	t.Helper()
	vars, err := composedotenv.UnmarshalWithLookup(strings.TrimPrefix(text, byteOrderMark), nil)
	if err != nil {
		t.Fatalf("Compose rejects %q: %v", text, err)
	}
	return vars
}

func ourVars(text string) map[string]string {
	vars := make(map[string]string)
	for _, entry := range Parse(text).Entries() {
		vars[entry.Key] = entry.Value
	}
	return vars
}

func TestCompose_ParseMatches(t *testing.T) {
	for _, text := range composeCorpus {
		t.Run(text, func(t *testing.T) {
			want := composeVars(t, text)
			if got := ourVars(text); !maps.Equal(got, want) {
				t.Fatalf("for %q:\nours    %q\ncompose %q", text, got, want)
			}
			if got := Parse(text).String(); got != text {
				t.Fatalf("expected the text to round-trip, got %q", got)
			}
		})
	}
}

var composeEditValues = []string{
	"new",
	"",
	"two words",
	"  padded  ",
	"a #b",
	"# starts with hash",
	"x\ny",
	"q\"uo'te",
	"back\\slash",
	"$HOME ${X} $$",
	"tab\there",
	"h\u00e9llo \u00a0 world",
	"A=B",
	"a: b",
}

func TestCompose_SetMatches(t *testing.T) {
	for _, text := range composeCorpus {
		for key := range composeVars(t, text) {
			for _, value := range composeEditValues {
				for _, public := range []bool{false, true} {
					doc := Parse(text)
					doc.Set(key, value, public)

					want := composeVars(t, text)
					want[key] = value
					got := composeVars(t, doc.String())
					if !maps.Equal(got, want) {
						t.Fatalf("Set(%q, %q, %v) on %q gave %q:\ncompose %q\nwant    %q",
							key, value, public, text, doc.String(), got, want)
					}
				}
			}
		}
	}
}

func TestCompose_SetNewKeyMatches(t *testing.T) {
	for _, text := range composeCorpus {
		for _, value := range composeEditValues {
			doc := Parse(text)
			doc.Set("NEW_KEY", value, false)

			want := composeVars(t, text)
			want["NEW_KEY"] = value
			got := composeVars(t, doc.String())
			if !maps.Equal(got, want) {
				t.Fatalf("Set(NEW_KEY, %q) on %q gave %q:\ncompose %q\nwant    %q",
					value, text, doc.String(), got, want)
			}
		}
	}
}

func TestCompose_RemoveMatches(t *testing.T) {
	for _, text := range composeCorpus {
		for key := range composeVars(t, text) {
			doc := Parse(text)
			doc.Remove(key)

			want := composeVars(t, text)
			delete(want, key)
			got := composeVars(t, doc.String())
			if !maps.Equal(got, want) {
				t.Fatalf("Remove(%q) on %q gave %q:\ncompose %q\nwant    %q",
					key, text, doc.String(), got, want)
			}
		}
	}
}
