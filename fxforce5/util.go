package fxforce5

// In returns true if needle is found in haystack.
func In(needle string, haystack []string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
