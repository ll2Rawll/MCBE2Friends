package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func privateFiles() {}

func lockInstance(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, syscall.Errno(32)) {
		return nil, errInUse
	} // ERROR_SHARING_VIOLATION
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func showMessage(message string) {
	title, _ := syscall.UTF16PtrFromString("Mcbe2Friends")
	body, _ := syscall.UTF16PtrFromString(message)
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0)
}
