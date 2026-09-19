//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func alert(message string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("ЯРУС")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		alert("Не удалось определить папку запуска ЯРУС.")
		return
	}
	root := filepath.Dir(executable)
	relative := filepath.Join("01-Windows", "YARUS-1.0.0-Windows-x64", "YARUS.exe")
	candidates := []string{
		filepath.Join(root, relative),
		filepath.Join(root, "YARUS-1.0.0-READY", relative),
	}
	for _, target := range candidates {
		if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
			command := exec.Command(target)
			command.Dir = filepath.Dir(target)
			if startErr := command.Start(); startErr != nil {
				alert("Не удалось запустить ЯРУС: " + startErr.Error())
			}
			return
		}
	}
	alert(fmt.Sprintf("Не найден YARUS.exe. Не отделяйте файл запуска от папки %s.", filepath.Base(root)))
}
