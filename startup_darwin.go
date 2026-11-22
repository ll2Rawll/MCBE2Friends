package main

import (
	"os"
	"os/user"
	"path/filepath"
)

func launchPlist(label, exe, root, account, home string) string {
	identity := ""
	if account != "" {
		identity = "<key>UserName</key><string>" + xmlValue(account) + "</string>\n<key>EnvironmentVariables</key><dict><key>HOME</key><string>" + xmlValue(home) + "</string></dict>\n"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + xmlValue(label) + `</string>
<key>ProgramArguments</key><array><string>` + xmlValue(exe) + `</string><string>-resume</string><string>-data-dir</string><string>` + xmlValue(root) + `</string></array>
<key>RunAtLoad</key><true/>
<key>ExitTimeOut</key><integer>40</integer>
` + identity + "</dict></plist>\n"
}

func registerStartup(root string, enable bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	label := "app.mcbe2friends." + profileID(root)
	entry := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	if !enable {
		return removeEntry(entry)
	}
	exe, err := startupExecutable(root)
	if err != nil {
		return err
	}
	if err := startupFile(entry, launchPlist(label, exe, root, "", "")); err != nil {
		return err
	}
	account, err := user.Current()
	if err != nil {
		return err
	}
	return startupFile(filepath.Join(root, "background", "start-before-sign-in.plist"), launchPlist(label+".boot", exe, root, account.Username, home))
}
