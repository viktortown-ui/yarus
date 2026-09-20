//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	Size uint32
	Data *byte
}

var crypt32 = syscall.NewLazyDLL("crypt32.dll")
var kernel32Protect = syscall.NewLazyDLL("kernel32.dll")

func blob(value []byte) dataBlob {
	if len(value) == 0 {
		return dataBlob{}
	}
	return dataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func protectJournalKey(key []byte) ([]byte, error) {
	input := blob(key)
	entropyBytes := []byte("YARUS journal key v1")
	entropy := blob(entropyBytes)
	var output dataBlob
	ok, _, callErr := crypt32.NewProc("CryptProtectData").Call(
		uintptr(unsafe.Pointer(&input)), 0, uintptr(unsafe.Pointer(&entropy)), 0, 0, 1, uintptr(unsafe.Pointer(&output)))
	if ok == 0 {
		return nil, callErr
	}
	defer kernel32Protect.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func unprotectJournalKey(value []byte) ([]byte, error) {
	input := blob(value)
	entropyBytes := []byte("YARUS journal key v1")
	entropy := blob(entropyBytes)
	var output dataBlob
	ok, _, callErr := crypt32.NewProc("CryptUnprotectData").Call(
		uintptr(unsafe.Pointer(&input)), 0, uintptr(unsafe.Pointer(&entropy)), 0, 0, 1, uintptr(unsafe.Pointer(&output)))
	if ok == 0 {
		if callErr == nil {
			callErr = errors.New("CryptUnprotectData failed")
		}
		return nil, callErr
	}
	defer kernel32Protect.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func replaceFileAtomic(source, destination string) error {
	src, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	ok, _, callErr := kernel32Protect.NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 0x1|0x8)
	if ok == 0 {
		return callErr
	}
	return nil
}
