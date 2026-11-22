package main

import (
	"encoding/json"
	"net/http"
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

// Opt-in on an isolated Windows runner: launch the shipped command through the
// real noninteractive boot task and verify it can fetch the pinned broadcaster.
func TestWindowsScheduledResume(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_BOOT_TASK") != "1" {
		t.Skip("requires permission to register a temporary Task Scheduler boot task")
	}
	root := t.TempDir()
	l, err := newLauncher(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.save(Settings{Host: "127.0.0.1", Port: 19132, Name: "Startup test"}); err != nil {
		t.Fatal(err)
	}
	if err = l.background.set(broadcastService, true); err != nil {
		t.Fatal(err)
	}
	if err = windowsStartupEntries(root, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "background", "Mcbe2Friends.exe")
	if out, err := exec.Command("go", "build", "-ldflags", "-H windowsgui", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build worker: %s %v", out, err)
	}
	task := "Mcbe2Friends-QA-" + profileID(root)
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("schtasks.exe", args...).CombinedOutput(); err != nil {
			t.Fatalf("Task Scheduler: %s %v", out, err)
		}
	}
	run("/Create", "/TN", task, "/XML", filepath.Join(root, "background", "start-before-sign-in.xml"))
	defer func() { _ = exec.Command("schtasks.exe", "/End", "/TN", task).Run(); run("/Delete", "/TN", task, "/F") }()
	run("/Run", "/TN", task)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	discovery := filepath.Join(root, "launcher.json")
	var instance Instance
	for until := time.Now().Add(45 * time.Second); time.Now().Before(until); {
		if i, up := liveInstance(client, discovery); up {
			instance = i
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if instance.Port == 0 {
		t.Fatal("scheduled worker did not open the expected profile")
	}
	post := func(path string) {
		t.Helper()
		req, _ := http.NewRequest("POST", instance.origin()+path, nil)
		req.Header.Set("Authorization", "Bearer "+instance.Token)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(path, res.StatusCode)
		}
	}
	defer func() {
		post("/api/stop")
		for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
			post("/api/quit")
			time.Sleep(200 * time.Millisecond)
			if _, err := os.Stat(discovery); os.IsNotExist(err) {
				return
			}
		}
		t.Error("idle scheduled worker did not exit")
	}()
	jar := filepath.Join(root, "components", "MCXboxBroadcastStandalone-"+buildNumber+".jar")
	for until := time.Now().Add(2 * time.Minute); time.Now().Before(until); {
		if _, err := os.Stat(jar); err == nil && verified(jar, jarDigest) {
			return
		}
		time.Sleep(350 * time.Millisecond)
	}
	t.Fatal("the scheduled worker did not download the verified broadcaster over HTTPS")
}
