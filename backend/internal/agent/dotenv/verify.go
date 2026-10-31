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

	for _, change := range changes {
		value, ok := newVars[change.Key]
		switch {
		case change.Delete && ok:
			return fmt.Errorf("verification failed: key %q is still present after deleting it", change.Key)
		case !change.Delete && (!ok || value != change.Value):
			return fmt.Errorf("verification failed: key %q does not have the requested value", change.Key)
		}
	}

	if oldErr != nil {
		return nil
	}
	want := make(map[string]struct{}, len(oldVars)+len(changes))
	for key := range oldVars {
		want[key] = struct{}{}
	}
	for _, change := range changes {
		if change.Delete {
			delete(want, change.Key)
		} else {
			want[change.Key] = struct{}{}
		}
	}
	if !slices.Equal(slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(newVars))) {
		return errors.New("verification failed: the edit changed keys that were not requested")
	}
	return nil
}
