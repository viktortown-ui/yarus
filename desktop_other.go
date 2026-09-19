//go:build !windows

package main

import "errors"

func desktopUI(address string) error {
	return errors.New("native desktop host is available in the Windows build")
}
func nativeAlert(message string) {}
