package dotenv

import (
	"strings"
	"testing"
)

func applyChanges(doc *Doc, changes []Change) {
	for _, change := range changes {
		if change.Delete {
			doc.Remove(change.Key)
		} else {
			doc.Set(change.Key, change.Value, false)
		}
	}
}

func TestVerifyEdit_AcceptsCorrectEdits(t *testing.T) {
	before := "# comment\n# orcacd:public\nA=1\nB=\"two words\"\nC=3\n"
	changes := []Change{
		{Key: "A", Value: "new $value \"quoted\" \\ \n second line\t"},
		{Key: "B", Delete: true},
		{Key: "D", Value: "added"},
	}

	doc := Parse(before)
	applyChanges(doc, changes)

	if err := VerifyEdit(before, doc.String(), changes); err != nil {
		t.Fatalf("expected the edit to verify, got %v", err)
	}
}

func TestVerifyEdit_RejectsWrongValue(t *testing.T) {
	changes := []Change{{Key: "A", Value: "wanted"}}

	err := VerifyEdit("A=1\n", "A=something else\n", changes)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "wanted") || strings.Contains(err.Error(), "something else") {
		t.Fatalf("error must not contain values: %v", err)
	}
}

func TestVerifyEdit_RejectsKeyStillPresent(t *testing.T) {
	changes := []Change{{Key: "A", Delete: true}}

	if err := VerifyEdit("A=1\n", "A=1\n", changes); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVerifyEdit_RejectsUnrequestedKeyChanges(t *testing.T) {
	changes := []Change{{Key: "A", Value: "2"}}

	if err := VerifyEdit("A=1\nB=1\n", "A=2\n", changes); err == nil {
		t.Fatal("expected an error for a key that disappeared")
	}
	if err := VerifyEdit("A=1\n", "A=2\nB=1\n", changes); err == nil {
		t.Fatal("expected an error for a key that appeared")
	}
}

func TestVerifyEdit_RejectsFileComposeCannotParse(t *testing.T) {
	changes := []Change{{Key: "A", Value: "2"}}

	if err := VerifyEdit("A=1\n", "A=\"2\nB=1\n", changes); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVerifyEdit_ReferencesToChangedKeysAreNotCompared(t *testing.T) {
	before := "A=1\nB=${A}\n"
	changes := []Change{{Key: "A", Value: "2"}}

	doc := Parse(before)
	applyChanges(doc, changes)

	if err := VerifyEdit(before, doc.String(), changes); err != nil {
		t.Fatalf("expected the edit to verify, got %v", err)
	}
}

func TestVerifyEdit_FileWithByteOrderMark(t *testing.T) {
	before := byteOrderMark + "A=1\nB=2\n"
	changes := []Change{{Key: "A", Value: "new"}, {Key: "B", Delete: true}}

	doc := Parse(before)
	applyChanges(doc, changes)

	if err := VerifyEdit(before, doc.String(), changes); err != nil {
		t.Fatalf("expected the edit to verify, got %v", err)
	}
	if err := VerifyEdit(before, byteOrderMark+"A=1\nB=2\n", changes); err == nil {
		t.Fatal("expected an unchanged file to fail verification")
	}
}

func TestVerifyEdit_AlreadyInvalidFileIsNotChecked(t *testing.T) {
	before := "A=\"unterminated\n"
	changes := []Change{{Key: "B", Value: "1"}}

	doc := Parse(before)
	applyChanges(doc, changes)

	if err := VerifyEdit(before, doc.String(), changes); err != nil {
		t.Fatalf("expected no error for a file Compose already rejects, got %v", err)
	}
}
