//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func detachWorker(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// Persist the directory entry as well as the file when committing a user's choice.
func commitFile(temporary, target string) error {
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer dir.Close()
	err = dir.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}

// Unix session managers request shutdown with SIGTERM.
func watchSessionEnd(cancel func()) func() { return func() {} }
