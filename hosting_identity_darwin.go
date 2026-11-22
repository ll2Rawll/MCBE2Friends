package main

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"time"
)

var platformUUID = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([0-9a-fA-F-]{36})"`)

func systemInstallationID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if match := platformUUID.FindSubmatch(output); err == nil && len(match) == 2 {
		return string(match[1]), nil
	}
	return "", errors.New("Premium could not read this Mac’s platform UUID.")
}
