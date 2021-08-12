package cmap

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	mInitSize = 1 << 4
)

// Map is a "thread" safe map of type AnyComparableType:Any.
// To avoid lock bottlenecks this map is dived to several map shards.
type Map struct {
	mu    sync.Mutex
	count int64
	node  unsafe.Pointer
}

type node struct {
	mask    uintptr
	buckets []bucket
}

type bucket struct {
	mu   sync.RWMutex
	init int64
	m    map[interface{}]interface{}
}

type entry struct {
	key, value interface{}
}

func New() *Map {
	m := Map{}
	n := m.getNode()
	n.initBuckets()
	return &m
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
// The ok result indicates whether value was found in the map.
func (m *Map) Load(key interface{}) (value interface{}, ok bool) {
	hash := chash(key)
	_, b := m.getNodeAndBucket(hash)
	value, ok = b.tryLoad(key)
	return
}

// Store sets the value for a key.
func (m *Map) Store(key, value interface{}) {
	hash := chash(key)
	n, b := m.getNodeAndBucket(hash)
	b.tryStore(m, n, key, value)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Map) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	hash := chash(key)
	n, b := m.getNodeAndBucket(hash)
	actual, loaded = b.tryLoadOrStore(m, n, key, value)
	return
}

// Delete deletes the value for a key.
func (m *Map) Delete(key interface{}) {
	m.LoadAndDelete(key)
}

// Delete deletes the value for a key.
func (m *Map) LoadAndDelete(key interface{}) (value interface{}, loaded bool) {
	hash := chash(key)
	n, b := m.getNodeAndBucket(hash)
	value, loaded = b.tryDelete(m, n, key)
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
func (m *Map) Range(f func(key, value interface{}) bool) {
	n := m.getNode()
	for i := range n.buckets {
		b := &(n.buckets[i])
		if !b.inited() {
			n.initBucket(uintptr(i))
		}
		for _, e := range b.clone() {
			if !f(e.key, e.value) {
				return
			}
		}
	}
}

// Len returns the number of elements within the map.
func (m *Map) Len() int {
	return int(atomic.LoadInt64(&m.count))
}

func (m *Map) getNodeAndBucket(hash uintptr) (n *node, b *bucket) {
	n = m.getNode()
	i := hash & n.mask
	b = &(n.buckets[i])
	if !b.inited() {
		n.initBucket(i)
	}
	return n, b
}

func (m *Map) getNode() *node {
	n := (*node)(atomic.LoadPointer(&m.node))
	if n == nil {
		m.mu.Lock()
		n = (*node)(atomic.LoadPointer(&m.node))
		if n == nil {
			n = &node{
				mask:    uintptr(mInitSize - 1),
				buckets: make([]bucket, mInitSize),
			}
			atomic.StorePointer(&m.node, unsafe.Pointer(n))
		}
		m.mu.Unlock()
	}
	return n
}

func (n *node) initBuckets() {
	for i := range n.buckets {
		n.initBucket(uintptr(i))
	}
}

func (n *node) initBucket(i uintptr) {
	b := &(n.buckets[i])
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inited() {
		return
	}

	b.m = make(map[interface{}]interface{})
	atomic.StoreInt64(&b.init, 1)
}

func (b *bucket) inited() bool {
	return atomic.LoadInt64(&b.init) == 1
}

func (b *bucket) clone() []entry {
	b.mu.RLock()
	entries := make([]entry, 0, len(b.m))
	for k, v := range b.m {
		entries = append(entries, entry{key: k, value: v})
	}
	b.mu.RUnlock()
	return entries
}

func (b *bucket) tryLoad(key interface{}) (value interface{}, ok bool) {
	b.mu.RLock()
	value, ok = b.m[key]
	b.mu.RUnlock()
	return
}

func (b *bucket) tryStore(m *Map, n *node, key, value interface{}) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	l0 := len(b.m) // Using length check existence is faster than accessing.
	b.m[key] = value
	l1 := len(b.m)
	if l0 == l1 {
		return true
	}
	atomic.AddInt64(&m.count, 1)
	// TODO grow

	return true
}

func (b *bucket) tryLoadOrStore(m *Map, n *node, key, value interface{}) (actual interface{}, loaded bool) {
	value, loaded = b.tryLoad(key)
	if loaded {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	actual, loaded = b.m[key]
	if loaded {
		return
	}
	b.m[key] = value
	atomic.AddInt64(&m.count, 1)
	// TODO grow

	return value, false
}

func (b *bucket) tryDelete(m *Map, n *node, key interface{}) (value interface{}, loaded bool) {
	value, loaded = b.tryLoad(key)
	if !loaded {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	value, loaded = b.m[key]
	if !loaded {
		return
	}
	delete(b.m, key)
	atomic.AddInt64(&m.count, -1)
	// TODO shrink

	return
}
