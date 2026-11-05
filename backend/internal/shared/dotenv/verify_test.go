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

func TestVerifyEdit_RejectsChangedUntouchedValues(t *testing.T) {
	changes := []Change{{Key: "A", Value: "3"}}

	tests := []struct {
		name   string
		before string
		after  string
	}{
		{"plain value", "A=1\nB=2\n", "A=3\nB=9\n"},
		{"multi-line value", "A=1\nB=\"x\ny\"\n", "A=3\nB=\"x\nz\"\n"},
		{"reference rewritten", "A=1\nB=${A}\n", "A=3\nB=${C}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifyEdit(tt.before, tt.after, changes); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestVerifyEdit_RejectsFileComposeCannotParse(t *testing.T) {
	changes := []Change{{Key: "A", Value: "2"}}

	if err := VerifyEdit("A=1\n", "A=\"2\nB=1\n", changes); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVerifyEdit_RejectsMissingKey(t *testing.T) {
	changes := []Change{{Key: "A", Value: "2"}}

	if err := VerifyEdit("A=1\n", "B=1\n", changes); err == nil {
		t.Fatal("expected an error for a key that is missing")
	}
}

func TestVerifyEdit_FileComposeAlreadyRejected(t *testing.T) {
	before := "A=\"1\nB=2\n"
	changes := []Change{{Key: "A", Value: "2"}}

	if err := VerifyEdit(before, "A=2\nB=2\n", changes); err != nil {
		t.Fatalf("expected the fixed file to verify, got %v", err)
	}
	if err := VerifyEdit(before, "A=3\nB=2\n", changes); err == nil {
		t.Fatal("expected an error for a wrong value")
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

func TestVerifyEdit_NoOpSetOnSubstitutedValue(t *testing.T) {
	tests := []struct {
		name   string
		before string
		key    string
		value  string
	}{
		{"reference", "B=1\nA=${B}\n", "A", "${B}"},
		{"escaped dollar", "A=a$$b\n", "A", "a$$b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := []Change{{Key: tt.key, Value: tt.value}}

			doc := Parse(tt.before)
			applyChanges(doc, changes)

			if got := doc.String(); got != tt.before {
				t.Fatalf("expected a no-op Set to leave the file as it is, got %q", got)
			}
			if err := VerifyEdit(tt.before, doc.String(), changes); err != nil {
				t.Fatalf("expected the no-op edit to verify, got %v", err)
			}
		})
	}
}

func TestVerifyEdit_NoOpSetWhileReferencedKeyChanges(t *testing.T) {
	tests := []struct {
		name    string
		before  string
		changes []Change
	}{
		{"referenced key set", "B=1\nA=${B}\n", []Change{{Key: "B", Value: "2"}, {Key: "A", Value: "${B}"}}},
		{"no-op first", "B=1\nA=${B}\n", []Change{{Key: "A", Value: "${B}"}, {Key: "B", Value: "2"}}},
		{"referenced key deleted", "B=1\nA=${B:-x}\n", []Change{{Key: "B", Delete: true}, {Key: "A", Value: "${B:-x}"}}},
		{"chain", "C=1\nB=${C}\nA=${B}\n", []Change{{Key: "C", Value: "2"}, {Key: "A", Value: "${B}"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse(tt.before)
			applyChanges(doc, tt.changes)

			if err := VerifyEdit(tt.before, doc.String(), tt.changes); err != nil {
				t.Fatalf("expected the edit to verify, got %v", err)
			}
		})
	}
}

func TestVerifyEdit_RejectsRewrittenSubstitutedValue(t *testing.T) {
	before := "B=1\nA=${B}\n"
	changes := []Change{{Key: "A", Value: "${B}"}}

	if err := VerifyEdit(before, "B=1\nA=\"\\${B}\"\n", changes); err == nil {
		t.Fatal("expected an error")
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
