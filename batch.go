package gokv

import "fmt"

// GetMultiple retrieves the values for the given keys from the store.
//
// It's meant for batch use cases like list pages that need to fetch many
// key-value pairs at once. The call is not all-or-nothing: keys for which
// no value exists (anymore) are returned in missing, while the values of
// all other keys are returned in found, so a single deleted key doesn't
// prevent the caller from getting the remaining ones.
//
// A non-nil error is only returned when the store itself fails
// (e.g. the backend is unreachable). In that case no partial results are
// returned, so an actual store failure is never mistaken for missing keys.
//
// Note: The keys are fetched one by one via the store's Get method,
// because the Store interface has no native batch operation.
func GetMultiple[T any](s Store, keys []string) (found map[string]T, missing []string, err error) {
	found = make(map[string]T, len(keys))
	for _, k := range keys {
		var v T
		keyFound, getErr := s.Get(k, &v)
		if getErr != nil {
			return nil, nil, fmt.Errorf("error getting key %q: %w", k, getErr)
		}
		if !keyFound {
			missing = append(missing, k)
			continue
		}
		found[k] = v
	}
	return found, missing, nil
}
