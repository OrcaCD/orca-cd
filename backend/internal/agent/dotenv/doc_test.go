package dotenv

import "testing"

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

	want := "# Database\n# orcacd:public\nHOST=db2\n\n# secret below\nPASSWORD=new\nOTHER='quoted'\n"
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

func TestDoc_KeyCountCountsDistinctKeys(t *testing.T) {
	if got := Parse("A=1\nB=2\nA=3\n# c\n").KeyCount(); got != 2 {
		t.Fatalf("expected 2 keys, got %d", got)
	}
}
