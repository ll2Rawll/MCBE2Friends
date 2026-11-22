//go:build !desktop

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
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
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && desktopWindowIsOpen(root) {
			// Wails on Linux returns 1 after focusing the existing window.
			return nil
		}
		return errors.New("The desktop window could not open. Use a graphical session with GTK 3 and WebKitGTK 4.1 installed.")
	}
	return nil
}

func desktopWindowIsOpen(root string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer conn.Close()
	id := strings.ReplaceAll("mcbe2friends-"+profileID(root), "-", "_")
	var open bool
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, "org.wails_app_"+id+".SingleInstance").Store(&open)
	return err == nil && open
}
