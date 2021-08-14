package cmap

import "unsafe"

func chash(i interface{}) uintptr {
	return nilinterhash(noescape(unsafe.Pointer(&i)), 0xdeadbeef)
}

// in runtime/alg.go
//
//go:linkname nilinterhash runtime.nilinterhash
func nilinterhash(p unsafe.Pointer, h uintptr) uintptr

//go:nocheckptr
//go:nosplit
func noescape(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x ^ 0)
}
