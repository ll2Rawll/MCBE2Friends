package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacStartupEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "a & b", "profile")
	if err := registerStartup(root, true); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, "Library", "LaunchAgents", "app.mcbe2friends."+profileID(root)+".plist")
	daemon := filepath.Join(root, "background", "start-before-sign-in.plist")
	for _, name := range []string{entry, daemon} {
		out, err := exec.Command("/usr/bin/plutil", "-extract", "ProgramArguments.3", "raw", "-o", "-", name).CombinedOutput()
		if err != nil || strings.TrimRight(string(out), "\n") != root {
			t.Fatalf("invalid launchd arguments %q: %v", out, err)
		}
	}
	out, err := exec.Command("/usr/bin/plutil", "-extract", "UserName", "raw", "-o", "-", daemon).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "" || strings.TrimSpace(string(out)) == "root" {
		t.Fatalf("boot account %q: %v", out, err)
	}
	if err := registerStartup(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatal("login item remained", err)
	}
}
