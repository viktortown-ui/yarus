//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func messageBox(message string, flags uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	text, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("ЯРУС")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), flags)
}

func alert(message string)  { messageBox(message, 0x10) }
func notice(message string) { messageBox(message, 0x40) }

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0700)
		}
		if relative == "server.lock" || relative == "OPEN-YARUS.url" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		temporary := target + ".tmp"
		output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			_ = os.Remove(temporary)
			return copyErr
		}
		if inputCloseErr != nil {
			_ = os.Remove(temporary)
			return inputCloseErr
		}
		if outputCloseErr != nil {
			_ = os.Remove(temporary)
			return outputCloseErr
		}
		return os.Rename(temporary, target)
	})
}

func releaseDataIsEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "README-НЕ-УДАЛЯТЬ.txt") {
			continue
		}
		return false
	}
	return true
}

func acquireMigrationLock(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func migratePreviousData(root, targetDirectory string) (bool, error) {
	newData := filepath.Join(targetDirectory, "data")
	if !releaseDataIsEmpty(newData) {
		return false, nil
	}
	oldRelative := filepath.Join("YARUS-1.1.1-READY", "01-Windows", "YARUS-1.1.1-Windows-x64", "data")
	var oldData, oldInstall string
	for _, base := range []string{filepath.Dir(root), root} {
		candidate := filepath.Join(base, oldRelative)
		journal, err := os.Stat(filepath.Join(candidate, "yarus.journal"))
		if err == nil && !journal.IsDir() && journal.Size() > 0 {
			oldData = candidate
			oldInstall = filepath.Dir(candidate)
			break
		}
	}
	if oldData == "" {
		return false, nil
	}
	lock, err := acquireMigrationLock(filepath.Join(oldData, "server.lock"))
	if err != nil {
		return false, fmt.Errorf("закройте ЯРУС 1.1.1 и повторите запуск: старая база сейчас открыта")
	}
	defer lock.Close()
	stage, err := os.MkdirTemp(targetDirectory, "data-migration-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage)
	if err = copyTree(oldData, stage); err != nil {
		return false, fmt.Errorf("не удалось скопировать прежнюю базу: %w", err)
	}
	// YARUS 1.1.1 could place WebView2 state beside YARUS.exe on some computers.
	// Prefer that active profile and move it under data, where 1.1.2 keeps it reliably.
	externalProfile := filepath.Join(oldInstall, "YARUS.exe.WebView2")
	if entries, profileErr := os.ReadDir(externalProfile); profileErr == nil && len(entries) > 0 {
		profileTarget := filepath.Join(stage, "desktop-webview")
		if err = os.RemoveAll(profileTarget); err != nil {
			return false, fmt.Errorf("не удалось подготовить профиль прежней версии: %w", err)
		}
		if err = copyTree(externalProfile, profileTarget); err != nil {
			return false, fmt.Errorf("не удалось скопировать сохранённый вход: %w", err)
		}
	}
	for _, required := range []string{"yarus.journal", "yarus.key"} {
		info, statErr := os.Stat(filepath.Join(stage, required))
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			return false, fmt.Errorf("в прежней версии отсутствует обязательный файл %s", required)
		}
	}
	if !releaseDataIsEmpty(newData) {
		return false, fmt.Errorf("новая папка данных изменилась во время обновления")
	}
	emptyBackup := newData + ".empty"
	_ = os.RemoveAll(emptyBackup)
	if err = os.Rename(newData, emptyBackup); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err = os.Rename(stage, newData); err != nil {
		_ = os.Rename(emptyBackup, newData)
		return false, err
	}
	_ = os.RemoveAll(emptyBackup)
	return true, nil
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		alert("Не удалось определить папку запуска ЯРУС.")
		return
	}
	root := filepath.Dir(executable)
	relative := filepath.Join("01-Windows", "YARUS-1.1.2-Windows-x64", "YARUS.exe")
	candidates := []string{
		filepath.Join(root, relative),
		filepath.Join(root, "YARUS-1.1.2-READY", relative),
	}
	for _, target := range candidates {
		if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
			migrated, migrationErr := migratePreviousData(root, filepath.Dir(target))
			if migrationErr != nil {
				alert("Обновление не запущено: " + migrationErr.Error() + "\n\nДанные не изменены.")
				return
			}
			if migrated {
				notice("Склад из ЯРУС 1.1.1 безопасно скопирован в 1.1.2.\n\nПроверьте данные в новой версии, прежде чем удалять старую папку.")
			}
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
