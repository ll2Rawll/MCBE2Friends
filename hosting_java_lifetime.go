package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed packaging/java-lifetime/worker-lifetime.jar
var workerLifetimeJar []byte

// The JVM verifies its original parent before loading the server. If the worker
// disappears even during startup, the game's shutdown hooks save the world.
func javaLifetimeOption(dir string) (string, error) {
	const name = ".mcbe2friends-worker.jar"
	path := filepath.Join(dir, name)
	saved, _ := os.ReadFile(path)
	if !bytes.Equal(saved, workerLifetimeJar) {
		if err := atomicWrite(path, workerLifetimeJar); err != nil {
			return "", err
		}
	}
	return "-javaagent:" + name + "=" + strconv.Itoa(os.Getpid()), nil
}
