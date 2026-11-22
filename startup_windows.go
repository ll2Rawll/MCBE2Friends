package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func registerStartup(root string, enable bool) error {
	startup, err := windows.KnownFolderPath(windows.FOLDERID_Startup, 0)
	if err != nil {
		return err
	}
	return windowsStartupEntries(root, startup, enable)
}

func windowsStartupEntries(root, startup string, enable bool) error {
	shortcut := filepath.Join(startup, profileID(root)+".lnk")
	if !enable {
		return removeEntry(shortcut)
	}
	exe, err := startupExecutable(root)
	if err != nil {
		return err
	}
	args := windows.ComposeCommandLine([]string{"-resume", "-data-dir", root})
	// The script is fixed. Paths travel as data and are never PowerShell source.
	const script = `$ErrorActionPreference='Stop'; $s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:M2F_SHORTCUT); $s.TargetPath=$env:M2F_BINARY; $s.Arguments=$env:M2F_ARGS; $s.WorkingDirectory=$env:M2F_DIR; $s.Description='Resume your Mcbe2Friends broadcast and server'; $s.Save()`
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(startup, 0700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	configureChild(cmd)
	cmd.Env = append(os.Environ(), "M2F_SHORTCUT="+shortcut, "M2F_BINARY="+exe, "M2F_ARGS="+args, "M2F_DIR="+root)
	if err := cmd.Run(); err != nil {
		return err
	}
	account, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	task := `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Restore this player's Mcbe2Friends services at boot.</Description></RegistrationInfo>
  <Triggers><BootTrigger><Enabled>true</Enabled></BootTrigger></Triggers>
  <Principals><Principal id="Player"><UserId>` + xmlValue(account.User.Sid.String()) + `</UserId><LogonType>S4U</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
  <Actions Context="Player"><Exec><Command>` + xmlValue(exe) + `</Command><Arguments>` + xmlValue(args) + `</Arguments><WorkingDirectory>` + xmlValue(root) + `</WorkingDirectory></Exec></Actions>
</Task>
`
	return startupFile(filepath.Join(root, "background", "start-before-sign-in.xml"), task)
}
