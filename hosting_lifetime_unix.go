//go:build linux || darwin

package main

import "os"

// Linux sets the parent-death signal before launch. macOS has no native Bedrock host.
func bindBedrockLifetime(_ *os.Process) (func(), error) {
	return func() {}, nil
}
