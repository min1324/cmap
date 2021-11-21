package cmap

type Interface interface {
	// Load returns the value stored in the map for a key, or nil if no
	// value is present.
	// The ok result indicates whether value was found in the map.
	Load(key interface{}) (value interface{}, ok bool)

	// Store sets the value for a key.
	Store(key, value interface{})

	// LoadOrStore returns the existing value for the key if present.
	// Otherwise, it stores and returns the given value.
	// The loaded result is true if the value was loaded, false if stored.
	LoadOrStore(key, value interface{}) (actual interface{}, loaded bool)

	// Delete deletes the value for a key.
	Delete(key interface{})

	// LoadAndDelete deletes the value for a key, returning the previous value if any.
	// The loaded result reports whether the key was present.
	LoadAndDelete(key interface{}) (value interface{}, loaded bool)

	// Range calls f sequentially for each key and value present in the map.
	// If f returns false, range stops the iteration.
	Range(f func(key, value interface{}) bool)

	// Count returns the number of elements within the map.
	Count() int64
}

// New return an initialize map
func New() Interface {
	return &Map{}
}

// NewFMap return an initialize fmap
func NewFMap() Interface {
	return &FMap{}
}

// NewCMap return an initialize cmap
func NewCMap() Interface {
	m := &CMap{}
	n := m.getNode()
	n.initBuckets()
	return m
}
