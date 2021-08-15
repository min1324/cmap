package cmap

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	mInitBit  = 4
	mInitSize = 1 << mInitBit
)

// Map is a "thread" safe map of type AnyComparableType:Any.
// To avoid lock bottlenecks this map is dived to several map shards.
type Map struct {
	mu    sync.Mutex
	count int64
	node  unsafe.Pointer
}

type node struct {
	mask    uintptr        // 1<<B - 1
	B       uint8          // log_2 of # of buckets (can hold up to loadFactor * 2^B items)
	resize  uint32         // 重新计算进程，0表示完成，1表示正在进行
	oldNode unsafe.Pointer // *node
	buckets []bucket
}

type bucket struct {
	mu       sync.RWMutex
	init     int32
	evacuted int32                       // 1 表示oldNode对应buckut已经迁移到新buckut
	frozen   int32                       // true表示当前bucket已经冻结，进行resize
	m        map[interface{}]interface{} //
}

// New return an initialize cmap
func New() *Map {
	m := &Map{}
	n := m.getNode()
	n.initBuckets()
	return m
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
	for {
		n, b := m.getNodeAndBucket(hash)
		if b.tryStore(m, n, key, value) {
			return
		}
		runtime.Gosched()
	}
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Map) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	hash := chash(key)
	var ok bool
	for {
		n, b := m.getNodeAndBucket(hash)
		actual, loaded, ok = b.tryLoadOrStore(m, n, key, value)
		if ok {
			return
		}
		runtime.Gosched()
	}
}

// Delete deletes the value for a key.
func (m *Map) Delete(key interface{}) {
	m.LoadAndDelete(key)
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (m *Map) LoadAndDelete(key interface{}) (value interface{}, loaded bool) {
	hash := chash(key)
	var ok bool
	for {
		n, b := m.getNodeAndBucket(hash)
		value, loaded, ok = b.tryLoadAndDelete(m, n, key)
		if ok {
			return
		}
		runtime.Gosched()
	}
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
		b := n.getBucket(uintptr(i))
		if !b.walk(f) {
			return
		}
	}
}

// Len returns the number of elements within the map.
func (m *Map) Len() int {
	return int(atomic.LoadInt64(&m.count))
}

func (m *Map) getNodeAndBucket(hash uintptr) (n *node, b *bucket) {
	n = m.getNode()
	b = n.getBucket(hash)
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
				B:       mInitBit,
				buckets: make([]bucket, mInitSize),
			}
			atomic.StorePointer(&m.node, unsafe.Pointer(n))
		}
		m.mu.Unlock()
	}
	return n
}

// give a hash key and return it's store bucket
func (n *node) getBucket(h uintptr) *bucket {
	return n.initBucket(h)
}

func (n *node) initBuckets() {
	for i := range n.buckets {
		n.initBucket(uintptr(i))
	}
	// empty oldNode
	atomic.StorePointer(&n.oldNode, nil)
	// finish all evacute
	atomic.StoreUint32(&n.resize, 0)
}

func (n *node) initBucket(i uintptr) *bucket {
	i = i & n.mask
	b := &(n.buckets[i])
	b.lazyinit()
	p := (*node)(atomic.LoadPointer(&n.oldNode))
	if p != nil && !b.hadEvacuted() {
		evacute(n, p, b, i)
	}
	return b
}

// evacute node.oldNode -> node
// i must be b==n.buckuts[i&n.mask]
func evacute(n, p *node, b *bucket, i uintptr) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hadEvacuted() || p == nil {
		return
	}
	if n.mask > p.mask {
		// grow
		pb := p.getBucket(i)
		for k, v := range pb.freeze() {
			h := chash(k)
			if h&n.mask == i {
				b.m[k] = v
			}
		}
	} else {
		// shrink
		pb0 := p.getBucket(i)
		for k, v := range pb0.freeze() {
			b.m[k] = v
		}
		pb1 := *p.getBucket(i + bucketShift(n.B))
		for k, v := range pb1.freeze() {
			b.m[k] = v
		}
	}
	atomic.StoreInt32(&b.evacuted, 1)
}

func (b *bucket) lazyinit() {
	if b.hadInited() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hadInited() {
		return
	}
	b.m = make(map[interface{}]interface{})
	atomic.StoreInt32(&b.init, 1)
}

func (b *bucket) hadInited() bool {
	return atomic.LoadInt32(&b.init) == 1
}

func (b *bucket) hadEvacuted() bool {
	return atomic.LoadInt32(&b.evacuted) == 1
}

func (b *bucket) hadFrozen() bool {
	return atomic.LoadInt32(&b.frozen) == 1
}

func (b *bucket) freeze() map[interface{}]interface{} {
	b.mu.Lock()
	atomic.StoreInt32(&b.frozen, 1)
	m := b.m
	b.mu.Unlock()
	return m
}

func (b *bucket) walk(f func(k, v interface{}) bool) (done bool) {
	// use in range
	type entry struct {
		key, value interface{}
	}
	b.mu.RLock()
	entries := make([]entry, 0, len(b.m))
	for k, v := range b.m {
		entries = append(entries, entry{key: k, value: v})
	}
	b.mu.RUnlock()
	for _, e := range entries {
		if !f(e.key, e.value) {
			return false
		}
	}
	return true
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
	if b.hadFrozen() {
		return false
	}

	l0 := len(b.m) // Using length check existence is faster than accessing.
	b.m[key] = value
	l1 := len(b.m)
	if l0 == l1 {
		return true
	}
	count := atomic.AddInt64(&m.count, 1)
	// grow
	if overLoadFactor(int64(l1), n.B) || overflowGrow(count, n.B) {
		growWork(m, n, n.B+1)
	}
	return true
}

func (b *bucket) tryLoadOrStore(m *Map, n *node, key, value interface{}) (actual interface{}, loaded, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hadFrozen() {
		return nil, false, false
	}
	actual, loaded = b.m[key]
	if loaded {
		return actual, loaded, true
	}
	b.m[key] = value
	count := atomic.AddInt64(&m.count, 1)

	// grow
	if overLoadFactor(int64(len(b.m)), n.B) || overflowGrow(count, n.B) {
		growWork(m, n, n.B+1)
	}
	return value, false, true
}

func (b *bucket) tryLoadAndDelete(m *Map, n *node, key interface{}) (actual interface{}, loaded, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hadFrozen() {
		return nil, false, false
	}
	actual, loaded = b.m[key]
	if !loaded {
		return nil, false, true
	}
	delete(b.m, key)
	count := atomic.AddInt64(&m.count, -1)

	// shrink
	if belowShrink(count, n.B) {
		growWork(m, n, n.B-1)
	}
	return actual, loaded, true
}

func growWork(m *Map, n *node, B uint8) {
	if !n.growing() && atomic.CompareAndSwapUint32(&n.resize, 0, 1) {
		nn := &node{
			mask:    bucketMask(B),
			B:       B,
			resize:  1,
			oldNode: unsafe.Pointer(n),
			buckets: make([]bucket, bucketShift(B)),
		}
		ok := atomic.CompareAndSwapPointer(&m.node, unsafe.Pointer(n), unsafe.Pointer(nn))
		if !ok {
			panic("BUG: failed swapping head")
		}
		go nn.initBuckets()
	}
}

func (n *node) growing() bool {
	return atomic.LoadPointer(&n.oldNode) != nil
}

// buckut len over loadfactor
func overLoadFactor(blen int64, B uint8) bool {
	if B > 15 {
		B = 15
	}
	return blen > int64(1<<(B+1)) && B < 31
}

// count overflow grow threshold
func overflowGrow(count int64, B uint8) bool {
	if B > 31 {
		return false
	}
	return count >= int64(1<<(2*B))
}

// count below shrink threshold
func belowShrink(count int64, B uint8) bool {
	if B-1 <= mInitBit {
		return false
	}
	return count < int64(1<<(B-1))
}

// bucketShift returns 1<<b, optimized for code generation.
func bucketShift(b uint8) uintptr {
	// Masking the shift amount allows overflow checks to be elided.
	return uintptr(1) << (b)
}

// bucketMask returns 1<<b - 1, optimized for code generation.
func bucketMask(b uint8) uintptr {
	return bucketShift(b) - 1
}
