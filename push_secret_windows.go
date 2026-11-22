//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

type pushBlob struct {
	Size uint32
	Data *byte
}

func cryptPush(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}
	in := pushBlob{Size: uint32(len(data)), Data: &data[0]}
	var out pushBlob
	name := "CryptProtectData"
	if decrypt {
		name = "CryptUnprotectData"
	}
	r, _, _ := syscall.NewLazyDLL("crypt32.dll").NewProc(name).Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("DPAPI failed")
	}
	defer syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func protectPush(b []byte) ([]byte, error)   { return cryptPush(b, false) }
func unprotectPush(b []byte) ([]byte, error) { return cryptPush(b, true) }
