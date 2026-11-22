//go:build !desktop

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// The portable Linux worker can start at boot on a machine without graphics
// libraries. Only the separate, packaged desktop window loads GTK and WebKit.
func showDesktop(root string, instance Instance) error {
	if runtime.GOOS != "linux" {
		return errors.New("This source build has no desktop window. Use the published download or build with -tags desktop,production.")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	program := filepath.Join(filepath.Dir(exe), "libexec", "Mcbe2Friends-desktop")
	if _, err := os.Stat(program); err != nil {
		return errors.New("The desktop window is missing. Extract the entire Linux archive, including its libexec folder.")
	}
	cmd := exec.Command(program, "-desktop-window", "-data-dir", root)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.New("The desktop window could not open. Use a graphical session with GTK 3 and WebKitGTK 4.1 installed.")
	}
	return nil
}
