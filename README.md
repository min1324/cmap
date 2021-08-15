# cmap 
[![Build Status](https://travis-ci.org/min1324/cmap.svg?branch=master)](https://travis-ci.org/min1324/cmap) [![Go Report Card](https://goreportcard.com/badge/github.com/min1324/cmap)](https://goreportcard.com/report/github.com/min1324/cmap)
ConcurrentMap (cmap) 是 **go** 分片加锁 map 的一种。它在通过减小锁的粒度和持有的时间来提高并发性。

## 原理

ConcurrentMap 和 MutexMap 主要区别就是围绕着锁的粒度以及如何锁。如图

![](doc/aab3105d-568e-3ce3-96c3-5ee2b854a494.jpeg)

左边便是MutexMap的实现方式---锁整个hash表；而右边则是ConcurrentMap的实现方式---锁桶（或段）。ConcurrentMap将hash表分为8个桶（默认值），诸如load,store,delete等常用操作只锁当前需要用到的桶。

 ConcurrentMap中主要实体类就是三个：ConcurrentMap（整个Hash表）,Node（节点），Bucket（桶），对应上面的图可以看出之间的关系。

## grow 和 shrink

### 扩容gorw

增加key后，hash表总key数量count满足条件：count > 1<<2*B，或在buckut储存的key数量size满足条件：size > 1<<(B+1)，就会对hash表进行扩容。

### 收缩shrink

删除key后，hash表总key数量count满足条件：count > initSize && count < 1<<(B-1)，hash表进行收缩。

**扩容收缩操作：**

1. 将 resize 置为1。
2. newLen = 1<<B，申请新 node 。
3. 将旧node疏散到新node。
4. 完成疏散后，将旧node置空，resize置0。


## compare
The `map` type in Go doesn't support concurrent reads and writes. 

`cmap(concurrent-map)` provides a high-performance solution to this by sharding the map with minimal time spent waiting for locks.

The `sync.Map` has a few key differences from this map. The stdlib `sync.Map` is designed for append-only scenarios.

 So if you want to use the map for something more like in-memory db, you might benefit from using our version. You can read more about it in the golang repo, for example [here](https://github.com/golang/go/issues/21035) and [here](https://stackoverflow.com/questions/11063473/map-with-concurrent-access)

_Here we fork some README document from [concurrent-map](https://github.com/orcaman/concurrent-map)_

## usage

Import the package:

```go
import (
	"github.com/min1324/cmap"
)

```

```bash
go get "github.com/min1324/cmap"
```

The package is now imported under the "cmap" namespace.

## example

```go

	// Create a new map.
	var m cmap.Cmap

	// Stores item within map, sets "bar" under key "foo"
	m.Store("foo", "bar")

	// Retrieve item from map.
	if tmp, ok := m.Load("foo"); ok {
		bar := tmp.(string)
	}

	// Deletes item under key "foo"
	m.Delete("foo")

```