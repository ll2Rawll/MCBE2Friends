package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	devicePattern    = regexp.MustCompile("^PC2-[A-Z2-7]{26}$")
	keyPattern       = regexp.MustCompile("^M2F2[A-Z2-7]{26}$")
	installationSalt = regexp.MustCompile("^(PC-[A-Z2-7]{16}|INSTALL2-[A-Z2-7]{52})$")
)

func installationCode(dir string) (string, error) {
	id, err := systemInstallationID()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "installation.txt")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		var salt [32]byte
		if _, err = rand.Read(salt[:]); err != nil {
			return "", err
		}
		data = []byte("INSTALL2-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(salt[:]) + "\n")
		err = atomicWrite(path, data)
	}
	if err != nil {
		return "", err
	}
	return boundPCCode(strings.TrimSpace(string(data)), runtime.GOOS, id)
}

// The install salt stays local. Neither the OS identifier nor the salt leaves the PC.
func boundPCCode(salt, platform, id string) (string, error) {
	if !installationSalt.MatchString(salt) {
		return "", errors.New("The installation file is damaged. Restore it from your backup.")
	}
	id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != 16 || id == strings.Repeat("0", 32) {
		return "", errors.New("Premium could not read a valid operating system installation ID.")
	}
	m := hmac.New(sha256.New, []byte(salt))
	m.Write([]byte("Mcbe2Friends PC v2\x00" + platform + "\x00"))
	m.Write(raw)
	return "PC2-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(m.Sum(nil)[:16]), nil
}
