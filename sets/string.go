// Package sets implements basic set types.
package sets

// String is map disguised as set.
type StringSet struct {
	set map[string]struct{}
}

// NewString returns an empty string set.
func NewStringSet(s ...string) *StringSet {
	ss := &StringSet{set: make(map[string]struct{})}
	for _, item := range s {
		ss.Add(item)
	}
	return ss
}

// Add adds a string to a set, returns true if added, false it it already existed (noop).
func (set *StringSet) Add(s string) bool {
	_, found := set.set[s]
	set.set[s] = struct{}{}
	return !found // False if it existed already
}

// Add adds a set of string to a set.
func (set *StringSet) AddAll(s ...string) bool {
	for _, item := range s {
		set.set[item] = struct{}{}
	}
	return true
}

// Contains returns true if given string is in the set, false otherwise.
func (set *StringSet) Contains(s string) bool {
	_, found := set.set[s]
	return found
}

// Size returns current number of elements in the set.
func (set *StringSet) Size() int {
	return len(set.set)
}

// Values returns the set values as a string slice.
func (set *StringSet) Values() (values []string) {
	for k := range set.set {
		values = append(values, k)
	}
	return values
}
