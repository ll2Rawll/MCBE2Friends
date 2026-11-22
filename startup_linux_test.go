package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxStartupEntries(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	root := filepath.Join(t.TempDir(), `100% $Money "blocks"`)
	if err := registerStartup(root, true); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(config, "systemd", "user", profileID(root)+".service")
	link := filepath.Join(filepath.Dir(unit), "default.target.wants", filepath.Base(unit))
	entry := filepath.Join(config, "autostart", profileID(root)+".desktop")
	for _, path := range []string{unit, link, entry} {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "/usr/bin/env -- ") || !strings.Contains(string(data), " -resume -data-dir ") {
			t.Fatal(path, err)
		}
	}
	info, err := os.Stat(filepath.Join(root, "background", "Mcbe2Friends"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("missing private executable", err)
	}
	if err := registerStartup(root, true); err != nil {
		t.Fatal(err)
	}
	if err := registerStartup(root, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{unit, link, entry} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("unexpected startup entry", path, err)
		}
	}
}
