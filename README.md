# cmap
ConcurrentMap (cmap) 是 **go** 分片加锁 map 的一种。它在通过减小锁的粒度和持有的时间来提高并发性。

## 原理

ConcurrentMap 和 MutexMap 主要区别就是围绕着锁的粒度以及如何锁。如图

![](doc/aab3105d-568e-3ce3-96c3-5ee2b854a494.jpeg)

左边便是MutexMap的实现方式---锁整个hash表；而右边则是ConcurrentMap的实现方式---锁桶（或段）。ConcurrentMap将hash表分为8个桶（默认值），诸如load,store,delete等常用操作只锁当前需要用到的桶。

 ConcurrentMap中主要实体类就是三个：ConcurrentMap（整个Hash表）,Segment（桶），HashEntry（节点），对应上面的图可以看出之间的关系。

## grow 和 shrink

### gorw

增加key后，hash表总key数量count满足条件：count > len(buckets) * overflowThreshold，或在buckut储存的key数量size满足条件：size > 2*overflowThreshold，就会对hash表进行扩容。

扩容操作：

1. 将 resizeInProgress 置为1。
2. newLen = len(buckets)<<1，申请新 node 为原来的2倍。
3. 

### shrink

删除key后，hash表总key数量count满足条件：count > initSize && count < shrinkThreshold，hash表进行收缩。