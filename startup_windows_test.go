package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsStartupEntries(t *testing.T) {
	startup := t.TempDir()
	root := filepath.Join(t.TempDir(), "a & b", "profile")
	if err := windowsStartupEntries(root, startup, true); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; $s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:M2F_TEST_LINK); if ($s.TargetPath -ne $env:M2F_TEST_EXE -or $s.Arguments -ne $env:M2F_TEST_ARGS) {throw 'Incorrect shortcut'}; $scheduler=New-Object -ComObject Schedule.Service; $scheduler.Connect(); $task=$scheduler.NewTask(0); $task.XmlText=[IO.File]::ReadAllText($env:M2F_TEST_XML); if ($task.Triggers.Item(1).Type -ne 8 -or $task.Principal.LogonType -ne 2 -or $task.Principal.RunLevel -ne 0 -or $task.Actions.Item(1).Path -ne $env:M2F_TEST_EXE) {throw 'Incorrect boot task'}`)
	link := filepath.Join(startup, profileID(root)+".lnk")
	cmd.Env = append(os.Environ(), "M2F_TEST_LINK="+link, "M2F_TEST_EXE="+filepath.Join(root, "background", "Mcbe2Friends.exe"), "M2F_TEST_ARGS="+windows.ComposeCommandLine([]string{"-resume", "-data-dir", root}), "M2F_TEST_XML="+filepath.Join(root, "background", "start-before-sign-in.xml"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native startup validation: %s, %v", out, err)
	}
	if err := windowsStartupEntries(root, startup, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(link); !os.IsNotExist(err) {
		t.Fatal("startup shortcut remained", err)
	}
}

func TestWindowsSessionEnd(t *testing.T) {
	ended := make(chan struct{})
	finish := watchSessionEnd(func() { close(ended) })
	defer finish()
	var hwnd uintptr
	sessionWindows.Range(func(key, value any) bool { hwnd = key.(uintptr); return false })
	if hwnd == 0 {
		t.Fatal("missing session notification window")
	}
	ui := windows.NewLazySystemDLL("user32.dll")
	if r, _, e := ui.NewProc("PostMessageW").Call(hwnd, 0x0016, 1, 0); r == 0 {
		t.Fatal(e)
	}
	select {
	case <-ended:
	case <-time.After(4 * time.Second):
		t.Fatal("session-end notification did not reach the worker")
	}
}
