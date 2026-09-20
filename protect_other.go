//go:build !windows

package main

import "os"

func protectJournalKey(key []byte) ([]byte, error)   { return append([]byte(nil), key...), nil }
func unprotectJournalKey(key []byte) ([]byte, error) { return append([]byte(nil), key...), nil }
func replaceFileAtomic(source, destination string) error {
	return os.Rename(source, destination)
}
