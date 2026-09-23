//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigratePreviousDataCopiesCurrentEncryptedStoreAndProfile(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "YARUS-1.2.0-READY")
	target := filepath.Join(root, "01-Windows", "YARUS-1.2.0-Windows-x64")
	oldInstall := filepath.Join(base, "YARUS-1.1.2-READY", "01-Windows", "YARUS-1.1.2-Windows-x64")
	old := filepath.Join(oldInstall, "data")
	oldProfile := filepath.Join(oldInstall, "YARUS.exe.WebView2")
	for _, directory := range []string{filepath.Join(target, "data"), filepath.Join(old, "desktop-webview", "IndexedDB"), filepath.Join(oldProfile, "IndexedDB")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(target, "data", "README-НЕ-УДАЛЯТЬ.txt"):      "empty release folder",
		filepath.Join(old, "yarus.journal"):                         "encrypted-journal",
		filepath.Join(old, "yarus.key"):                             "protected-key",
		filepath.Join(old, "desktop-webview", "IndexedDB", "state"): "stale-profile",
		filepath.Join(oldProfile, "IndexedDB", "state"):             "saved-login",
		filepath.Join(old, "OPEN-YARUS.url"):                        "stale",
		filepath.Join(old, "server.lock"):                           "",
	}
	for path, value := range files {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sourceVersion, err := migratePreviousData(root, target)
	if err != nil || sourceVersion != "1.1.2" {
		t.Fatalf("migration failed: source=%q err=%v", sourceVersion, err)
	}
	for relative, expected := range map[string]string{
		"yarus.journal": "encrypted-journal",
		"yarus.key":     "protected-key",
		filepath.Join("desktop-webview", "IndexedDB", "state"): "saved-login",
	} {
		value, readErr := os.ReadFile(filepath.Join(target, "data", relative))
		if readErr != nil || string(value) != expected {
			t.Fatalf("bad migrated %s: %q %v", relative, value, readErr)
		}
	}
	if _, err = os.Stat(filepath.Join(target, "data", "OPEN-YARUS.url")); !os.IsNotExist(err) {
		t.Fatal("stale launch URL was copied")
	}
	if value, err := os.ReadFile(filepath.Join(old, "yarus.journal")); err != nil || string(value) != "encrypted-journal" {
		t.Fatal("source backup changed")
	}
	again, err := migratePreviousData(root, target)
	if err != nil || again != "" {
		t.Fatalf("second migration must not overwrite data: source=%q err=%v", again, err)
	}
}
