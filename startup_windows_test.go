package main

import (
	"encoding/json"
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
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; $s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:M2F_TEST_LINK); $scheduler=New-Object -ComObject Schedule.Service; $scheduler.Connect(); $task=$scheduler.NewTask(0); $task.XmlText=[IO.File]::ReadAllText($env:M2F_TEST_XML); @{Shortcut=$s.TargetPath;Args=$s.Arguments;TaskExe=$task.Actions.Item(1).Path;TaskArgs=$task.Actions.Item(1).Arguments;Boot=$task.Triggers.Item(1).Type;Logon=$task.Principal.LogonType;Level=$task.Principal.RunLevel} | ConvertTo-Json -Compress`)
	link := filepath.Join(startup, profileID(root)+".lnk")
	cmd.Env = append(os.Environ(), "M2F_TEST_LINK="+link, "M2F_TEST_XML="+filepath.Join(root, "background", "start-before-sign-in.xml"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native startup validation: %s, %v", out, err)
	}
	var actual struct {
		Shortcut, Args, TaskExe, TaskArgs string
		Boot, Logon, Level                int
	}
	if err := json.Unmarshal(out, &actual); err != nil {
		t.Fatalf("native startup settings: %s, %v", out, err)
	}
	if actual.Boot != 8 || actual.Logon != 2 || actual.Level != 0 {
		t.Fatalf("wrong startup trigger or account permissions: %+v", actual)
	}
	samePath := func(want, got string) {
		t.Helper()
		a, e1 := os.Stat(want)
		b, e2 := os.Stat(got)
		if e1 != nil || e2 != nil || !os.SameFile(a, b) {
			t.Fatalf("startup path: want %q, got %q (%v, %v)", want, got, e1, e2)
		}
	}
	for _, command := range [][2]string{{actual.Shortcut, actual.Args}, {actual.TaskExe, actual.TaskArgs}} {
		exe, args := command[0], command[1]
		samePath(filepath.Join(root, "background", "Mcbe2Friends.exe"), exe)
		argv, err := windows.DecomposeCommandLine("Mcbe2Friends " + args)
		if err != nil || len(argv) != 4 || argv[1] != "-resume" || argv[2] != "-data-dir" {
			t.Fatalf("startup arguments: %q (%v)", args, err)
		}
		samePath(root, argv[3])
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
