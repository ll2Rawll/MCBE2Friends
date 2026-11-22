package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Settings struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Name       string `json:"name"`
	AutoFriend bool   `json:"autoFriend"`
}

var domainLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func (s Settings) normalized() (Settings, error) {
	s.Host = strings.TrimSpace(s.Host)
	s.Name = strings.TrimSpace(s.Name)
	if s.Host == "" {
		return s, errors.New("Enter your server’s address.")
	}
	if strings.ContainsAny(s.Host, "/?#@") {
		return s, errors.New("Use a hostname or IP address, such as play.example.net. Leave out https:// and any path.")
	}
	// Accept the address players often paste from a server listing.
	if host, port, err := net.SplitHostPort(s.Host); err == nil {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return s, errors.New("Use a port from 1 to 65535.")
		}
		s.Host, s.Port = host, int(n)
	} else if strings.HasPrefix(s.Host, "[") && strings.HasSuffix(s.Host, "]") {
		s.Host = s.Host[1 : len(s.Host)-1]
	}
	if ip := net.ParseIP(s.Host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() {
			return s, errors.New("Enter the address players connect to, such as a server IP or hostname.")
		}
		s.Host = ip.String()
	} else {
		s.Host = strings.TrimSuffix(s.Host, ".")
		labels := strings.Split(s.Host, ".")
		if len(s.Host) > 253 {
			return s, errors.New("The server address is too long.")
		}
		allNumeric := len(labels) == 4
		for _, label := range labels {
			if !domainLabel.MatchString(label) {
				return s, errors.New("Use a hostname or IP address, such as play.example.net. Leave out https:// and any path.")
			}
			if strings.Trim(label, "0123456789") != "" {
				allNumeric = false
			}
		}
		if allNumeric {
			return s, errors.New("Check the IPv4 address. Each number must be between 0 and 255.")
		}
		s.Host = strings.ToLower(s.Host)
	}
	if s.Port < 1 || s.Port > 65535 {
		return s, errors.New("Use a port from 1 to 65535.")
	}
	if utf8.RuneCountInString(s.Name) > 64 || strings.IndexFunc(s.Name, unicode.IsControl) != -1 {
		return s, errors.New("Use up to 64 characters on one line for the server name.")
	}
	return s, nil
}

func atomicWrite(path string, contents []byte) error {
	return atomicWriteMode(path, contents, 0600)
}

func atomicWriteMode(path string, contents []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".saving-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(contents)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return commitFile(file.Name(), path)
}

func readSettings(root string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Settings{Port: 19132, AutoFriend: true}, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s.normalized()
}

// The upstream v9 loader fills unset keys with its own defaults. The launcher
// owns this isolated working directory and never edits another installation.
func (s Settings) yaml() []byte {
	label := s.Name
	if label == "" {
		label = s.Host
	}
	return []byte(fmt.Sprintf(`config-version: 9
session:
  remote-address: %s
  remote-port: "%d"
  query-server: true
  config-fallback: true
  session-info:
    host-name: %s
    world-name: %s
friend-sync:
  auto-friend: %t
  initial-invite: false
  expiry:
    enabled: false
debug-mode: false
suppress-session-update-message: false
`, strconv.Quote(s.Host), s.Port, strconv.Quote(label), strconv.Quote(label), s.AutoFriend))
}
