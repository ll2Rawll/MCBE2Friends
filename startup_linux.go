package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func linuxArgument(value string, desktop bool) string {
	// Both launchers expand percent markers. Desktop Entry also has a
	// key-file escaping pass before its Exec argument parser.
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "$$", "%", "%%")
	if desktop {
		r = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`", "%", "%%")
	}
	value = r.Replace(value)
	if desktop {
		value = strings.ReplaceAll(value, "\\", "\\\\")
	}
	return "\"" + value + "\""
}

func registerStartup(root string, enable bool) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	name := profileID(root)
	units := filepath.Join(base, "systemd", "user")
	unit := filepath.Join(units, name+".service")
	link := filepath.Join(units, "default.target.wants", name+".service")
	desktop := filepath.Join(base, "autostart", name+".desktop")
	if !enable {
		return errors.Join(removeEntry(link), removeEntry(unit), removeEntry(desktop))
	}
	exe, err := startupExecutable(root)
	if err != nil {
		return err
	}
	if strings.ContainsAny(root, "\r\n\t") {
		return errors.New("Use a profile folder without line breaks or tabs for automatic startup.")
	}
	// systemd restricts characters in its executable field. env passes the
	// actual profile path as an ordinary argument, without invoking a shell.
	cmd := "/usr/bin/env -- " + linuxArgument(exe, false) + " -resume -data-dir " + linuxArgument(root, false)
	service := "[Unit]\nDescription=Mcbe2Friends background hosting\n\n[Service]\nType=exec\nExecStart=" + cmd + "\nKillMode=mixed\nTimeoutStopSec=40s\n\n[Install]\nWantedBy=default.target\n"
	deskCmd := "/usr/bin/env -- " + linuxArgument(exe, true) + " -resume -data-dir " + linuxArgument(root, true)
	entry := "[Desktop Entry]\nType=Application\nVersion=1.0\nName=Mcbe2Friends\nComment=Resume your broadcast and Bedrock server\nExec=" + deskCmd + "\nTerminal=false\nNoDisplay=false\n"
	// Some desktop distributions have no systemd user manager. If both
	// entry points are active, the profile lock selects a single worker.
	if err := startupFile(desktop, entry); err != nil {
		return err
	}
	if err := startupFile(unit, service); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		return err
	}
	if dest, err := os.Readlink(link); err == nil {
		if dest == unit {
			return nil
		}
		return fmt.Errorf("The startup link %s points to a different unit.", link)
	}
	return os.Symlink(unit, link)
}
