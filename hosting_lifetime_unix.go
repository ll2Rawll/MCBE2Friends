//go:build linux || darwin

package main

import "os"

// Linux sets the parent-death signal before launch. the macOS Java host checks its original worker inside the JVM.
func bindChildLifetime(_ *os.Process) (func(), error) {
	return func() {}, nil
}
