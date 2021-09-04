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

type CMap struct {
	mu    sync.Mutex
	count uint32         // number of element
	node  unsafe.Pointer // *node
}

type node struct {
	mask   uintptr          // 1<<B - 1
	B      uint8            // log_2 of # of buckets (can hold up to loadFactor * 2^B items)
	resize uint32           // 重新计算进程，0表示完成，1表示正在进行
	data   []unsafe.Pointer // *bucket
}

type bucket struct {
	// something diy
	m Map
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
// The ok result indicates whether value was found in the map.
func (m *CMap) Load(key interface{}) (value interface{}, ok bool) {
	hash := chash(key)
	_, b := m.getNodeAndBucket(hash)
	value, ok = b.tryLoad(key)
	return
}

// Store sets the value for a key.
func (m *CMap) Store(key, value interface{}) {
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
func (m *CMap) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
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
func (m *CMap) Delete(key interface{}) {
	m.LoadAndDelete(key)
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (m *CMap) LoadAndDelete(key interface{}) (value interface{}, loaded bool) {
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

func (m *CMap) getNodeAndBucket(hash uintptr) (n *node, b *bucket) {
	for {
		n = m.getNode()
		b = n.getBucket(hash)
		if b != nil {
			break
		}
		runtime.Gosched()
		// evacuted old bucket
		// wait until init new bucket
	}
	return n, b
}

func (m *CMap) getNode() *node {
	n := (*node)(atomic.LoadPointer(&m.node))
	if n == nil {
		m.mu.Lock()
		n = (*node)(atomic.LoadPointer(&m.node))
		if n == nil {
			n = &node{
				mask: uintptr(mInitSize - 1),
				B:    mInitBit,
				data: make([]unsafe.Pointer, mInitSize),
			}
			for i := 0; i < mInitSize; i++ {
				b := new(bucket)
				atomic.StorePointer(&n.data[i], unsafe.Pointer(b))
			}
			atomic.StorePointer(&m.node, unsafe.Pointer(n))
		}
		m.mu.Unlock()
	}
	return n
}

func (n *node) getBucket(i uintptr) *bucket {
	return (*bucket)(atomic.LoadPointer(&n.data[i&n.mask]))
}

func (b *bucket) tryLoad(key interface{}) (value interface{}, ok bool) {
	return b.m.Load(key)
}

func (b *bucket) tryStore(m *CMap, n *node, key, value interface{}) bool {
	_, loaded, _ := b.tryLoadOrStore(m, n, key, value)
	if loaded {
		b.m.Store(key, value)
	}
	return true
}

func (b *bucket) tryLoadOrStore(m *CMap, n *node, key, value interface{}) (actual interface{}, loaded, ok bool) {
	actual, loaded = b.m.LoadOrStore(key, value)
	if loaded {
		return actual, loaded, true
	}
	// grow
	if overflowGrow(atomic.AddUint32(&m.count, 1), n.B) {
		growWork(m, n, n.B+1)
	}
	return actual, loaded, true
}

func (b *bucket) tryLoadAndDelete(m *CMap, n *node, key interface{}) (actual interface{}, loaded, ok bool) {
	actual, loaded = b.m.LoadAndDelete(key)
	if !loaded {
		atomic.AddUint32(&m.count, ^uint32(0))
	}
	return actual, loaded, true
}

func growWork(m *CMap, n *node, B uint8) {
	if !atomic.CompareAndSwapUint32(&n.resize, 0, 1) {
		return
	}
	nn := &node{
		mask:   bucketMask(B),
		B:      B,
		resize: 1,
		data:   make([]unsafe.Pointer, bucketShift(B)),
	}
	// cas node
	ok := atomic.CompareAndSwapPointer(&m.node, unsafe.Pointer(n), unsafe.Pointer(nn))
	if !ok {
		panic("BUG: failed swapping head")
	}
	// evacute old node to new node
	go func() {
		oLen := bucketShift(n.B)
		for i := 0; i < int(oLen); i++ {
			newBucket := new(bucket)
			oldBucket := n.getBucket(uintptr(i))
			// #issue01 concurrent delete or store err
			oldBucket.m.walkLocketInFreeze(func(key, value interface{}) bool {
				h := chash(key)
				if h&nn.mask != uintptr(i) {
					newBucket.m.Store(key, value)
					oldBucket.m.deleteLocked(key)
				}
				return true
			})
			atomic.StorePointer(&nn.data[i+int(oLen)], unsafe.Pointer(newBucket))
			atomic.StorePointer(&nn.data[i], unsafe.Pointer(oldBucket))
		}
		atomic.AddUint32(&nn.resize, 0)
	}()

}

// buckut len over loadfactor
func overLoadFactor(blen uint32, B uint8) bool {
	if B > 15 {
		B = 15
	}
	return blen > uint32(1<<(B+1)) && B < 31
}

// count overflow grow threshold
func overflowGrow(count uint32, B uint8) bool {
	if B > 31 {
		return false
	}
	return count >= uint32(1<<(2*B))
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
