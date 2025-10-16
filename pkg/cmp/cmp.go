package cmp

// SliceEqualUnordered returns true if every element in a can be matched with an element in b, established by cmp.
// Each element in b can only be matched once.
// Additionally, the lengths of a and b must match, so each element in b also matches in element in a.
// If an element in a matches multiple elements in b, then the first available one is matched.
func SliceEqualUnordered[T any](a, b []T, cmp func(x, y T) bool) bool {
	if len(a) != len(b) {
		return false
	}
	bUsed := map[int]any{}
	for i := range a {
		found := false
		for j := range b {
			if _, used := bUsed[j]; used {
				continue
			}
			if cmp(a[i], b[j]) {
				bUsed[j] = nil
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// SliceEqual returns true if a and b contain the same elements.
// A nil slice and empty slice are considered equal.
func SliceEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PtrValEqual returns true if both a and b are nil or their values are equal.
func PtrValEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// PtrValEqualFn return true if both a and b are nil or their values satisfy cmp.
func PtrValEqualFn[T any](a, b *T, cmp func(x, y T) bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return cmp(*a, *b)
}

// LenSlicePtr return len(*ts) or 0 in case ts is nil.
func LenSlicePtr[T any](ts *[]T) int {
	if ts == nil {
		return 0
	}
	return len(*ts)
}

// Unpack returns the value that t points to or T's zero value if t is nil.
func UnpackPtr[T any](t *T) T {
	var r T
	if t != nil {
		r = *t
	}
	return r
}
