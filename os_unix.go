//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func privateFiles() { syscall.Umask(0077) }

func lockInstance(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errInUse
		}
		return nil, err
	}
	return f, nil
}

func showMessage(message string) {
	if runtime.GOOS == "darwin" {
		exec.Command("osascript", "-e", "on run argv", "-e", `display dialog (item 1 of argv) with title "Mcbe2Friends" buttons {"OK"}`, "-e", "end run", "--", message).Run()
	} else {
		exec.Command("zenity", "--info", "--title=Mcbe2Friends", "--no-markup", "--text="+message).Run()
	}
}
