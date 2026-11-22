package main

import (
	"errors"
	"syscall"
)

func bedrockPortOptions(fd uintptr) error {
	// Winsock's SO_EXCLUSIVEADDRUSE is the inverse of SO_REUSEADDR.
	const exclusiveAddrUse = -5
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, exclusiveAddrUse, 1)
}

func isPortInUse(err error) bool {
	return errors.Is(err, syscall.Errno(10048)) || errors.Is(err, syscall.Errno(10013))
}
