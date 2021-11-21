package cmap

import (
	"sync"
	"sync/atomic"
)

const (
	bBit          = 5
	bMask uintptr = 1<<bBit - 1
)

// FMap has fixation len bucket map
type FMap struct {
	count  int64 // number of element
	bucket [bMask + 1]sync.Map
}

func (m *FMap) getBucket(i uintptr) *sync.Map {
	return &m.bucket[i&bMask]
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
// The ok result indicates whether value was found in the map.
func (m *FMap) Load(key interface{}) (value interface{}, ok bool) {
	hash := chash(key)
	b := m.getBucket(hash)
	return b.Load(key)
}

// Store sets the value for a key.
func (m *FMap) Store(key, value interface{}) {
	hash := chash(key)
	b := m.getBucket(hash)
	_, loaded := b.Load(key)
	b.Store(key, value)
	if !loaded {
		atomic.AddInt64(&m.count, 1)
	}
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *FMap) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	hash := chash(key)
	b := m.getBucket(hash)
	actual, loaded = b.LoadOrStore(key, value)
	if !loaded {
		atomic.AddInt64(&m.count, 1)
	}
	return
}

// Delete deletes the value for a key.
func (m *FMap) Delete(key interface{}) {
	m.LoadAndDelete(key)
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (m *FMap) LoadAndDelete(key interface{}) (value interface{}, loaded bool) {
	hash := chash(key)
	b := m.getBucket(hash)
	value, loaded = b.LoadAndDelete(key)
	if loaded {
		atomic.AddInt64(&m.count, ^int64(0))
	}
	return
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot of the Map's
// contents: no key will be visited more than once, but if the value for any key
// is stored or deleted concurrently, Range may reflect any mapping for that key
// from any point during the Range call.
//
// Range may be O(N) with the number of elements in the map even if f returns
// false after a constant number of calls.
func (m *FMap) Range(f func(key, value interface{}) bool) {
	var flag = true
	for i := 0; i <= len(m.bucket); i++ {
		b := m.getBucket(uintptr(i))
		b.Range(func(key, value interface{}) bool {
			flag = f(key, value)
			return flag
		})
		if !flag {
			return
		}
	}
}

// Count returns the number of elements within the map.
func (m *FMap) Count() int64 {
	return atomic.LoadInt64(&m.count)
}
