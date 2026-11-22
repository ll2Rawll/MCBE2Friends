package main

import (
	"errors"
	"os"
	"strings"
)

func systemInstallationID() (string, error) {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(path); err == nil {
			id := strings.TrimSpace(string(b))
			if len(id) == 32 {
				return id, nil
			}
		}
	}
	return "", errors.New("Premium needs a persistent Linux machine ID. Check this PC's operating system installation.")
}
