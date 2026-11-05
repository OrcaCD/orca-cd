package dotenv

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	composedotenv "github.com/compose-spec/compose-go/v2/dotenv"
)

type Change struct {
	Key    string
	Value  string
	Delete bool
}

// VerifyEdit parses the file before and after an edit with the Compose parser
// and checks that the edit did what was asked
func VerifyEdit(before, after string, changes []Change) error {
	before = strings.TrimPrefix(before, byteOrderMark)
	after = strings.TrimPrefix(after, byteOrderMark)

	oldVars, oldErr := composedotenv.UnmarshalWithLookup(before, nil)
	newVars, newErr := composedotenv.UnmarshalWithLookup(after, nil)
	if newErr != nil {
		if oldErr != nil {
			// Compose already rejects the file, there is nothing to compare.
			return nil
		}
		return errors.New("the edited file would no longer be valid")
	}

	var rawBefore map[string]string
	if oldErr == nil {
		rawBefore = rawValues(before)
	}

	unchanged := func(change Change) bool {
		raw, existed := rawBefore[change.Key]
		return !change.Delete && existed && raw == change.Value
	}
	othersChange := slices.ContainsFunc(changes, func(change Change) bool { return !unchanged(change) })

	for _, change := range changes {
		value, ok := newVars[change.Key]
		switch {
		case change.Delete && ok:
			return fmt.Errorf("verification failed: key %q is still present after deleting it", change.Key)
		case change.Delete:
		case !ok:
			return fmt.Errorf("verification failed: key %q does not have the requested value", change.Key)
		default:
			want := change.Value
			if unchanged(change) && strings.Contains(change.Value, "$") {
				if othersChange {
					continue
				}
				want = oldVars[change.Key]
			}
			if value != want {
				return fmt.Errorf("verification failed: key %q does not have the requested value", change.Key)
			}
		}
	}

	if oldErr != nil {
		return nil
	}
	wantKeys := make(map[string]struct{}, len(oldVars)+len(changes))
	for key := range oldVars {
		wantKeys[key] = struct{}{}
	}
	touched := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		touched[change.Key] = struct{}{}
		if change.Delete {
			delete(wantKeys, change.Key)
		} else {
			wantKeys[change.Key] = struct{}{}
		}
	}
	if !maps.EqualFunc(wantKeys, newVars, func(struct{}, string) bool { return true }) {
		return errors.New("verification failed: the edit changed keys that were not requested")
	}

	rawAfter := rawValues(after)
	for key, raw := range rawBefore {
		if _, ok := touched[key]; ok {
			continue
		}
		// Values with $ may reference a changed key, so only their text is compared.
		if rawAfter[key] != raw || (!strings.Contains(raw, "$") && newVars[key] != oldVars[key]) {
			return errors.New("verification failed: the edit changed the value of a key that was not requested")
		}
	}
	return nil
}

func rawValues(text string) map[string]string {
	entries := Parse(text).Entries()
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		values[entry.Key] = entry.Value
	}
	return values
}
