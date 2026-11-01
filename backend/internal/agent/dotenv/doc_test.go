package dotenv

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDoc_MultilineQuotedValue(t *testing.T) {
	text := "# orcacd:public\nCERT=\"line1\nline2\"\nAFTER=1\n"
	doc := Parse(text)

	entries := entriesByKey(doc)
	if entries["CERT"].Value != "line1\nline2" || entries["AFTER"].Value != "1" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	doc.Set("CERT", "short", true)
	if got, want := doc.String(), "# orcacd:public\nCERT=short\nAFTER=1\n"; got != want {
		t.Fatalf("expected the whole multi-line value to be replaced:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetPreservesEverythingElse(t *testing.T) {
	doc := Parse("# Database\n# orcacd:public\nHOST=db\n\n# secret below\nPASSWORD=old # keep\nOTHER='quoted'\n")

	doc.Set("PASSWORD", "new", false)
	doc.Set("HOST", "db2", true)

	want := "# Database\n# orcacd:public\nHOST=db2\n\n# secret below\nPASSWORD=new # keep\nOTHER='quoted'\n"
	if got := doc.String(); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetKeepsLineWithUnchangedValueAndFlag(t *testing.T) {
	text := "# orcacd:public\nA=\"x y\"\nB='secret'\n"
	doc := Parse(text)

	doc.Set("A", "x y", true)
	doc.Set("B", "secret", false)

	if doc.String() != text {
		t.Fatalf("expected an unchanged key to keep its formatting, got %q", doc.String())
	}
}

func TestDoc_SetTogglesMarker(t *testing.T) {
	doc := Parse("A=1\n# orcacd:public\nB=2\n")

	doc.Set("A", "1", true)
	doc.Set("B", "2", false)

	if got, want := doc.String(), "# orcacd:public\nA=1\nB=2\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetTogglesMarkerWithoutRewritingValue(t *testing.T) {
	doc := Parse("B=1\nA=${B}\n# orcacd:public\nC=\"x y\"\nD=a$$b\n")

	doc.Set("A", "${B}", true)
	doc.Set("C", "x y", false)
	doc.Set("D", "a$$b", true)

	want := "B=1\n# orcacd:public\nA=${B}\nC=\"x y\"\n# orcacd:public\nD=a$$b\n"
	if got := doc.String(); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetKeepsCommentsAroundKey(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		value  string
		public bool
		want   string
	}{
		{"value change, comment above", "# note\nA=1\n", "2", false, "# note\nA=2\n"},
		{"value change, comment below", "A=1\n# note\nB=2\n", "9", false, "A=9\n# note\nB=2\n"},
		{"made public, comment above", "# note\nA=1\n", "1", true, "# note\n# orcacd:public\nA=1\n"},
		{"made public, comment below", "A=1\n# note\nB=2\n", "1", true, "# orcacd:public\nA=1\n# note\nB=2\n"},
		{"made secret, comment above", "# note\n# orcacd:public\nA=1\n", "1", false, "# note\nA=1\n"},
		{"made secret, comment below", "# orcacd:public\nA=1\n# note\nB=2\n", "1", false, "A=1\n# note\nB=2\n"},
		{"value change, inline comment on public key", "# orcacd:public\nA=1 # note\n", "2", true, "# orcacd:public\nA=2 # note\n"},
		{"comment between marker and key", "# orcacd:public\n# note\nA=1\n", "2", false, "# orcacd:public\n# note\nA=2\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.text)

			doc.Set("A", tt.value, tt.public)

			if got := doc.String(); got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestDoc_RemoveKeepsCommentsAroundKey(t *testing.T) {
	doc := Parse("# above\n# orcacd:public\nA=1\n# below\nB=2\n")

	doc.Remove("A")

	if got, want := doc.String(), "# above\n# below\nB=2\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetKeepsExportPrefixAndInlineComment(t *testing.T) {
	doc := Parse("A=1 # note\nexport C=2\n")

	doc.Set("A", "2", false)
	doc.Set("C", "3", false)

	if got, want := doc.String(), "A=2 # note\nexport C=3\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetKeepsLineStyle(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		value string
		want  string
	}{
		{"quoted value with comment", "A=\"x y\" # note\n", "z", "A=z # note\n"},
		{"multi-line value with comment", "A=\"x\ny\" # note\nB=1\n", "z", "A=z # note\nB=1\n"},
		{"empty value with comment", "A=1 # note\n", "", "A=\"\" # note\n"},
		{"crlf", "export A=1 # note\r\nB=2\r\n", "z", "export A=z # note\r\nB=2\r\n"},
		{"yaml style", "A: 1\n", "z", "A: z\n"},
		{"indented and spaced", "  A = 1\n", "z", "  A = z\n"},
		{"hash is not a comment", "A=a#b\n", "z", "A=z\n"},
		{"single quoted value with comment", "A='x' # note\n", "z", "A=z # note\n"},
		{"new value needs quotes", "A=1 # note\n", "a #b", "A=\"a #b\" # note\n"},
		{"new value is multi-line", "A=1 # note\n", "x\ny", "A=\"x\\ny\" # note\n"},
		{"comment without space after quote", "A=\"x\"# note\n", "z", "A=z # note\n"},
		{"trailing whitespace without comment", "A=1   \n", "z", "A=z\n"},
		{"duplicate key rewrites only the last", "A=1 # first\nA=2 # second\n", "z", "A=1 # first\nA=z # second\n"},
		{"no-break space before quoted value", "A= \"x\"\n", "z", "A= z\n"},
		{"no-break space, unchanged value", "A= \"x\"\n", "x", "A= \"x\"\n"},
		{"no-break space before unquoted value", "A= x\n", "z", "A= z\n"},
		{"export with tab", "export\tA=1 # note\n", "z", "export\tA=z # note\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.text)

			doc.Set("A", tt.value, false)

			if got := doc.String(); got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			if err := VerifyEdit(tt.text, doc.String(), []Change{{Key: "A", Value: tt.value}}); err != nil {
				t.Fatalf("expected the edit to verify, got %v", err)
			}
		})
	}
}

func TestDoc_EditLineWithSeveralStatements(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		changes []Change
		want    string
	}{
		{"set first", "A=\"x\" B=2\n", []Change{{Key: "A", Value: "z"}}, "A=\"z\" B=2\n"},
		{"set second", "A=\"x\" B=2\n", []Change{{Key: "B", Value: "3"}}, "A=\"x\" B=3\n"},
		{"set both", "A=\"x\" B=2\n", []Change{{Key: "A", Value: "z"}, {Key: "B", Value: "3"}}, "A=\"z\" B=3\n"},
		{"remove second", "A=\"x\" B=2\n", []Change{{Key: "B", Delete: true}}, "A=\"x\"\n"},
		{"remove first", "A=\"x\" B=2\n", []Change{{Key: "A", Delete: true}}, " B=2\n"},
		{"remove middle", "A=\"x\" B=\"y\" C=3\n", []Change{{Key: "B", Delete: true}}, "A=\"x\" C=3\n"},
		{"multi-line first", "A=\"x\ny\" B=2\n", []Change{{Key: "A", Value: "z"}}, "A=\"z\" B=2\n"},
		{"word that is not an assignment", "A=\"x\" junk\n", []Change{{Key: "A", Value: "z"}}, "A=\"z\" junk\n"},
		{"crlf", "A=\"x\" B=2\r\nC=3\r\n", []Change{{Key: "B", Value: "9"}}, "A=\"x\" B=9\r\nC=3\r\n"},
		{"crlf remove last", "A=\"x\" B=2\r\nC=3\r\n", []Change{{Key: "B", Delete: true}}, "A=\"x\"\r\nC=3\r\n"},
		{"crlf remove first", "A=\"x\" B=2\r\nC=3\r\n", []Change{{Key: "A", Delete: true}}, " B=2\r\nC=3\r\n"},
		{"remove last of chain", "A=\"x\" B=\"y\" C=3\nD=4\n", []Change{{Key: "C", Delete: true}}, "A=\"x\" B=\"y\"\nD=4\n"},
		{"remove all", "A=\"x\" B=2\nC=3\n", []Change{{Key: "A", Delete: true}, {Key: "B", Delete: true}}, "C=3\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.text)
			applyChanges(doc, tt.changes)

			if got := doc.String(); got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			if err := VerifyEdit(tt.text, doc.String(), tt.changes); err != nil {
				t.Fatalf("expected the edit to verify, got %v", err)
			}
		})
	}
}

func TestDoc_SetPublicOnStatementAfterQuote(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		value string
		want  string
	}{
		{"lf", "A=\"x\" B=2\nC=3\n", "2", "A=\"x\"\n# orcacd:public\n B=2\nC=3\n"},
		{"crlf", "A=\"x\" B=2\r\nC=3\r\n", "2", "A=\"x\"\r\n# orcacd:public\r\n B=2\r\nC=3\r\n"},
		{"followed by another statement", "A=\"x\" B=\"y\" C=3\n", "y", "A=\"x\"\n# orcacd:public\n B=\"y\" C=3\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.text)

			doc.Set("B", tt.value, true)

			if got := doc.String(); got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			if !entriesByKey(Parse(doc.String()))["B"].Public {
				t.Fatal("expected B to be public")
			}
		})
	}
}

func TestDoc_SetAppendsNewKeys(t *testing.T) {
	doc := Parse("A=1")

	doc.Set("PUBLIC", "p", true)
	doc.Set("SECRET", "s", false)

	if got, want := doc.String(), "A=1\n# orcacd:public\nPUBLIC=p\nSECRET=s\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}

	empty := Parse("")
	empty.Set("A", "1", false)
	if empty.String() != "A=1\n" {
		t.Fatalf("got %q", empty.String())
	}
}

func TestDoc_SetKeepsLineEndings(t *testing.T) {
	doc := Parse("A=1\r\nB=2\r\n")

	doc.Set("C", "3", true)

	if got, want := doc.String(), "A=1\r\nB=2\r\n# orcacd:public\r\nC=3\r\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_RemoveDeletesKeyAndMarker(t *testing.T) {
	doc := Parse("# keep\n# orcacd:public\nA=1\nB=2\nA=3\n# orcacd:public\nC=4\n")

	doc.Remove("A")
	doc.Remove("MISSING")

	if got, want := doc.String(), "# keep\nB=2\n# orcacd:public\nC=4\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDoc_SetSecretRemovesAllMarkersAboveKey(t *testing.T) {
	doc := Parse("# orcacd:public\n# orcacd:public\nA=1\n")

	doc.Set("A", "1", false)

	if got, want := doc.String(), "A=1\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
	if entriesByKey(Parse(doc.String()))["A"].Public {
		t.Fatal("expected A to be a secret")
	}
}

func TestDoc_RemoveDoesNotLeakMarkersToNextKey(t *testing.T) {
	doc := Parse("# orcacd:public\n# orcacd:public\nA=1\nB=secret\n")

	doc.Remove("A")

	if got, want := doc.String(), "B=secret\n"; got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
	if entriesByKey(Parse(doc.String()))["B"].Public {
		t.Fatal("expected B to stay a secret")
	}
}

func TestDoc_SetAfterStrayTrailingMarker(t *testing.T) {
	doc := Parse("A=1\n# orcacd:public\n")

	doc.Set("SECRET", "s", false)

	entries := entriesByKey(Parse(doc.String()))
	if entries["SECRET"].Public {
		t.Fatalf("expected a new secret not to pick up the stray marker:\n%q", doc.String())
	}

	doc = Parse("A=1\n# orcacd:public\n")
	doc.Set("PUBLIC", "p", true)

	if !entriesByKey(Parse(doc.String()))["PUBLIC"].Public {
		t.Fatalf("expected PUBLIC to be public:\n%q", doc.String())
	}
}

func TestDoc_EntriesScalesWithFileSize(t *testing.T) {
	const keys = 100_000
	var text strings.Builder
	for i := range keys {
		fmt.Fprintf(&text, "K%d=1\n", i)
	}
	doc := Parse(text.String())

	start := time.Now()
	entries := doc.Entries()
	elapsed := time.Since(start)

	if len(entries) != keys {
		t.Fatalf("expected %d entries, got %d", keys, len(entries))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Entries took %v for %d keys", elapsed, keys)
	}
}

func TestDoc_ManyStatementsOnOneLineScale(t *testing.T) {
	const statements = 50_000
	text := strings.Repeat(`A="x" `, statements) + "\n"

	start := time.Now()
	doc := Parse(text)
	parseTime := time.Since(start)

	start = time.Now()
	rendered := doc.String()
	renderTime := time.Since(start)

	if rendered != text {
		t.Fatal("expected the text to round-trip")
	}
	if parseTime > 2*time.Second {
		t.Errorf("Parse took %v for %d statements on one line", parseTime, statements)
	}
	if renderTime > 2*time.Second {
		t.Errorf("String took %v for %d statements on one line", renderTime, statements)
	}
}

func TestDoc_KeyCountCountsDistinctKeys(t *testing.T) {
	if got := Parse("A=1\nB=2\nA=3\n# c\n").KeyCount(); got != 2 {
		t.Fatalf("expected 2 keys, got %d", got)
	}
}
