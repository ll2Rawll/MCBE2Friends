//go:build linux || darwin

package main

import (
	"errors"
	"syscall"
)

func bedrockPortOptions(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
}

func isPortInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
