package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
)

func startupFile(path string, contents string) error {
	return startupBytes(path, []byte(contents))
}

func startupBytes(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, contents) {
		return nil
	}
	return atomicWrite(path, contents)
}

func removeEntry(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func xmlValue(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
