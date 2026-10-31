package dotenv

import (
	"maps"
	"slices"
	"testing"
)

func entriesByKey(doc *Doc) map[string]Entry {
	byKey := make(map[string]Entry)
	for _, entry := range doc.Entries() {
		byKey[entry.Key] = entry
	}
	return byKey
}

// assertValues checks that text has exactly the given keys and values.
func assertValues(t *testing.T, text string, want map[string]string) {
	t.Helper()
	got := make(map[string]string)
	for _, entry := range Parse(text).Entries() {
		got[entry.Key] = entry.Value
	}
	if !maps.Equal(got, want) {
		t.Fatalf("for %q:\ngot  %q\nwant %q", text, got, want)
	}
}

func TestParse_MarkerMakesVariable(t *testing.T) {
	doc := Parse("# orcacd:public\nLOG_LEVEL=debug\nDB_PASSWORD=hunter2\n")

	entries := entriesByKey(doc)
	if !entries["LOG_LEVEL"].Public || entries["LOG_LEVEL"].Value != "debug" {
		t.Fatalf("expected LOG_LEVEL to be a variable, got %+v", entries["LOG_LEVEL"])
	}
	if entries["DB_PASSWORD"].Public {
		t.Fatal("expected a key without a marker to be a secret")
	}
}

func TestParse_MarkerMustBeDirectlyAboveTheKey(t *testing.T) {
	doc := Parse("# orcacd:public\n\nBLANK_LINE_BETWEEN=1\n# orcacd:public\n# other comment\nCOMMENT_BETWEEN=2\n#orcacd:public\nNO_SPACE=3\n")

	entries := entriesByKey(doc)
	if entries["BLANK_LINE_BETWEEN"].Public || entries["COMMENT_BETWEEN"].Public {
		t.Fatal("expected a marker that is not directly above a key to be ignored")
	}
	if !entries["NO_SPACE"].Public {
		t.Fatal("expected '#orcacd:public' to be a marker")
	}
}

func TestParse_ExistingFileDefaultsToSecrets(t *testing.T) {
	doc := Parse("A=1\nexport B=2\nC = 3\n")

	for _, entry := range doc.Entries() {
		if entry.Public {
			t.Fatalf("expected %s to be a secret", entry.Key)
		}
	}
	if got := len(doc.Entries()); got != 3 {
		t.Fatalf("expected 3 entries, got %d", got)
	}
}

func TestParse_Values(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{`K=plain`, "plain"},
		{`K=`, ""},
		{`K=  spaced out  `, "spaced out"},
		{`K=value # comment`, "value"},
		{`K=a#b`, "a#b"},
		{`K="double quoted # not a comment"`, "double quoted # not a comment"},
		{`K='single quoted'`, "single quoted"},
		{`K='no \n escape'`, `no \n escape`},
		{`K="line\nbreak"`, "line\nbreak"},
		{`K="say \"hi\""`, `say "hi"`},
		{`K="cost \$5"`, "cost $5"},
		{`K="back\\slash"`, `back\slash`},
		{`K="quoted" # trailing comment`, "quoted"},
		{`K="  padded  "`, "  padded  "},
		{`K=""`, ""},
		{`K=''`, ""},
		{`K= # starts with a comment`, "# starts with a comment"},
		{`K=a #c`, "a"},
		{`K="a"   `, "a"},
		{`K=a=b`, "a=b"},
		{`K=a\b`, `a\b`},
		{`K=a"b`, `a"b`},
		{`K=a'b`, "a'b"},
		{`K='a"b'`, `a"b`},
		{`K="a'b"`, "a'b"},
		{`K='it\'s'`, "it's"},
		{`K='a\\b'`, `a\\b`},
		{`K="tab\there"`, "tab\there"},
		{`K="\0101"`, "A"},
		{`K="\x41 é"`, `\x41 é`},
		{`K="ünï ✓"`, "ünï ✓"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			doc := Parse("# orcacd:public\n" + tt.line + "\n")
			entry := entriesByKey(doc)["K"]
			if entry.Value != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, entry.Value)
			}
		})
	}
}

func TestParse_KeepsNonUTF8Bytes(t *testing.T) {
	text := "A=\xff\nB=\"\xfe\"\n"

	assertValues(t, text, map[string]string{"A": "\xff", "B": "\xfe"})
	if got := Parse(text).String(); got != text {
		t.Fatalf("expected the bytes to round-trip, got %q", got)
	}
}

func TestParse_MarkerVariants(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		public bool
	}{
		{"extra spaces after the hash", "#   orcacd:public\nK=1\n", true},
		{"indented marker", "   # orcacd:public\nK=1\n", true},
		{"tab indented marker", "\t# orcacd:public\nK=1\n", true},
		{"trailing spaces", "# orcacd:public   \nK=1\n", true},
		{"above an export line", "# orcacd:public\nexport K=1\n", true},
		{"above an indented key", "# orcacd:public\n  K=1\n", true},
		{"above a yaml style line", "# orcacd:public\nK: 1\n", true},
		{"above a multi-line value", "# orcacd:public\nK=\"a\nb\"\n", true},
		{"extra text after the marker", "# orcacd:public extra\nK=1\n", false},
		{"different case", "# ORCACD:PUBLIC\nK=1\n", false},
		{"double hash", "## orcacd:public\nK=1\n", false},
		{"marker as the value", "K=# orcacd:public\n", false},
		{"marker as inline comment", "K=1 # orcacd:public\n", false},
		{"marker below the key", "K=1\n# orcacd:public\n", false},
		{"line we do not understand in between", "# orcacd:public\nnot an assignment\nK=1\n", false},
		{"duplicate with marker only on the first", "# orcacd:public\nK=1\nK=2\n", false},
		{"duplicate with marker only on the last", "K=1\n# orcacd:public\nK=2\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, found := entriesByKey(Parse(tt.text))["K"]
			if !found {
				t.Fatalf("expected K to be listed in %q", tt.text)
			}
			if entry.Public != tt.public {
				t.Fatalf("expected public=%v for %q, got %v", tt.public, tt.text, entry.Public)
			}
		})
	}
}

func TestParse_MarkerWithoutKeyDoesNothing(t *testing.T) {
	text := "A=1\n# orcacd:public\n"

	doc := Parse(text)
	if entries := doc.Entries(); len(entries) != 1 || entries[0].Public {
		t.Fatalf("unexpected entries %+v", entries)
	}
	if doc.String() != text {
		t.Fatalf("expected the text to be unchanged, got %q", doc.String())
	}
}

func TestParse_MarkerAppliesOnlyToTheNextKey(t *testing.T) {
	entries := entriesByKey(Parse("# orcacd:public\nA=1\nB=2\n"))

	if !entries["A"].Public || entries["B"].Public {
		t.Fatalf("expected only A to be public, got %+v", entries)
	}
}

func TestParse_KeyForms(t *testing.T) {
	tests := []struct {
		name string
		text string
		want map[string]string
	}{
		{"yaml style", "K: v\n", map[string]string{"K": "v"}},
		{"yaml style without space", "K:v\n", map[string]string{"K": "v"}},
		{"colon comes first", "URL:http://h=1\n", map[string]string{"URL": "http://h=1"}},
		{"equals comes first", "URL=http://h:80\n", map[string]string{"URL": "http://h:80"}},
		{"export with two spaces", "export  K=v\n", map[string]string{"K": "v"}},
		{"export with a tab", "export\tK=v\n", map[string]string{"K": "v"}},
		{"key that starts with export", "exportK=v\n", map[string]string{"exportK": "v"}},
		{"key named export", "export=1\n", map[string]string{"export": "1"}},
		{"tab indented", "\tK=v\n", map[string]string{"K": "v"}},
		{"spaces around equals", "A = 1\nB= 2\nC =3\n", map[string]string{"A": "1", "B": "2", "C": "3"}},
		{"dots, dashes, digits, brackets", "a.b-c=1\n1A=2\nlist[0]=3\n", map[string]string{"a.b-c": "1", "1A": "2", "list[0]": "3"}},
		{"unicode key", "ключ=1\n", map[string]string{"ключ": "1"}},
		{"no separator", "JUSTAKEY\nK=v\n", map[string]string{"K": "v"}},
		{"empty key", "=1\nK=v\n", map[string]string{"K": "v"}},
		{"space in key", "A B=1\nK=v\n", map[string]string{"K": "v"}},
		{"invalid characters in key", "A$=1\nA#=2\nA\"=3\nK=v\n", map[string]string{"K": "v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValues(t, tt.text, tt.want)
		})
	}
}

func TestParse_UnlistedLinesStayUnchanged(t *testing.T) {
	text := "JUSTAKEY\n=1\nA B=1\nK=v\n"

	doc := Parse(text)
	doc.Set("K", "new", false)

	if got, want := doc.String(), "JUSTAKEY\n=1\nA B=1\nK=new\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParse_EntriesAreOrderedByLastAssignment(t *testing.T) {
	var keys []string
	for _, entry := range Parse("A=1\nB=2\nA=3\n").Entries() {
		keys = append(keys, entry.Key+"="+entry.Value)
	}

	if want := []string{"B=2", "A=3"}; !slices.Equal(keys, want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
}

func TestParse_MultilineValues(t *testing.T) {
	tests := []struct {
		name string
		text string
		want map[string]string
	}{
		{"with a blank line", "K=\"a\n\nb\"\nL=1\n", map[string]string{"K": "a\n\nb", "L": "1"}},
		{"single quotes", "K='a\nb'\nL=1\n", map[string]string{"K": "a\nb", "L": "1"}},
		{"two in a row", "K=\"a\nb\"\nL=\"c\nd\"\nM=1\n", map[string]string{"K": "a\nb", "L": "c\nd", "M": "1"}},
		{"backslash at the end of a line", "K=\"a\\\nb\"\n", map[string]string{"K": "a\\\nb"}},
		{"key inside the value is not a key", "K=\"a\nINNER=1\nb\"\n", map[string]string{"K": "a\nINNER=1\nb"}},
		{"marker inside the value is not a marker", "K=\"a\n# orcacd:public\nb\"\nL=1\n", map[string]string{"K": "a\n# orcacd:public\nb", "L": "1"}},
		{"closing quote on a line of its own", "K=\"a\n\"\nL=1\n", map[string]string{"K": "a\n", "L": "1"}},
		{"closed by the quote of a later line", "A=\"x\nB=\"y\"\n", map[string]string{"A": "x\nB="}},
		{"unterminated at the end of the file", "K=\"a\nb", map[string]string{}},
		{"unterminated before another assignment", "K=\"a\nL=1\n", map[string]string{"L": "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValues(t, tt.text, tt.want)
			if got := Parse(tt.text).String(); got != tt.text {
				t.Fatalf("expected the text to round-trip, got %q", got)
			}
		})
	}
}

func TestParse_MarkerOnlyAppliesToMultilineKey(t *testing.T) {
	entries := entriesByKey(Parse("# orcacd:public\nK=\"a\nb\"\nL=1\n"))

	if !entries["K"].Public || entries["L"].Public {
		t.Fatalf("expected only K to be public, got %+v", entries)
	}
}

func TestParse_CRLF(t *testing.T) {
	text := "# orcacd:public\r\nA=1\r\nB=\"x y\"\r\nC=3 # c\r\nD='q'\r\nE=\r\nM=\"a\r\nb\"\r\nN=1\r\n"

	doc := Parse(text)
	assertValues(t, text, map[string]string{"A": "1", "B": "x y", "C": "3", "D": "q", "E": "", "M": "a\r\nb", "N": "1"})
	if entries := entriesByKey(doc); !entries["A"].Public || entries["B"].Public {
		t.Fatalf("expected the marker to work with CRLF, got %+v", entries)
	}
	if doc.String() != text {
		t.Fatalf("expected the text to round-trip, got %q", doc.String())
	}
}

func TestParse_CRLFWithoutFinalNewline(t *testing.T) {
	text := "A=1\r\nB=2"

	assertValues(t, text, map[string]string{"A": "1", "B": "2"})
	if got := Parse(text).String(); got != text {
		t.Fatalf("expected the text to round-trip, got %q", got)
	}
}

func TestParse_ByteOrderMark(t *testing.T) {
	t.Run("before a key", func(t *testing.T) {
		text := "\ufeffA=1\nB=2\n"

		assertValues(t, text, map[string]string{"A": "1", "B": "2"})
		if got := Parse(text).String(); got != text {
			t.Fatalf("expected the text to round-trip, got %q", got)
		}
	})

	t.Run("before a marker", func(t *testing.T) {
		entries := entriesByKey(Parse("\ufeff# orcacd:public\nA=1\nB=2\n"))

		if !entries["A"].Public || entries["B"].Public {
			t.Fatalf("expected only A to be public, got %+v", entries)
		}
	})

	t.Run("is kept by edits", func(t *testing.T) {
		doc := Parse("\ufeffA=1\nB=2\n")

		doc.Set("A", "x", false)
		doc.Set("C", "3", false)

		if got, want := doc.String(), "\ufeffA=x\nB=2\nC=3\n"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("alone", func(t *testing.T) {
		if got := Parse("\ufeff").String(); got != "\ufeff" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestParse_UnterminatedQuoteIsLeftAlone(t *testing.T) {
	doc := Parse("BROKEN=\"never closed\nOK=1\n")

	if _, found := entriesByKey(doc)["BROKEN"]; found {
		t.Fatal("expected an unterminated value to be ignored")
	}
	if entriesByKey(doc)["OK"].Value != "1" {
		t.Fatal("expected the following line to be parsed")
	}
	if doc.String() != "BROKEN=\"never closed\nOK=1\n" {
		t.Fatalf("expected the text to be unchanged, got %q", doc.String())
	}
}

func TestParse_LastAssignmentWins(t *testing.T) {
	doc := Parse("# orcacd:public\nK=first\nK=second\n")

	entry := entriesByKey(doc)["K"]
	if entry.Value != "second" || entry.Public {
		t.Fatalf("expected the last assignment (a secret) to win, got %+v", entry)
	}
	if len(doc.Entries()) != 1 {
		t.Fatalf("expected the key once, got %d entries", len(doc.Entries()))
	}
}

func TestParse_RoundTripWithoutChangesIsIdentical(t *testing.T) {
	for _, text := range []string{
		"",
		"\n",
		"# header\n\nA=1\n   # indented\nB = \"x y\"  # c\nnot an assignment\n",
		"A=1",
		"A=1\r\nB=2\r\n",
	} {
		if got := Parse(text).String(); got != text {
			t.Fatalf("expected %q to round-trip, got %q", text, got)
		}
	}
}
