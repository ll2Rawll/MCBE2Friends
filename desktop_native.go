//go:build desktop

package main

import (
	"io/fs"
	"path/filepath"

	"mcbe2friends/desktopwindow"
)

func showDesktop(root string, instance Instance) error {
	config, err := launcherWindow(root, instance)
	if err != nil {
		return err
	}
	return desktopwindow.Show(config)
}

func launcherWindow(root string, instance Instance) (desktopwindow.Window, error) {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return desktopwindow.Window{}, err
	}
	icon, err := fs.ReadFile(assets, "assets/app-icon.png")
	if err != nil {
		return desktopwindow.Window{}, err
	}
	return desktopwindow.Window{
		Title: "Mcbe2Friends", ID: "mcbe2friends-" + profileID(root), DataPath: filepath.Join(root, "desktop-webview"),
		Width: 1200, Height: 820, Assets: assets, Icon: icon, Handler: desktopAPI(instance),
		OnClose: func() { closeDesktopControls(instance) },
	}, nil
}
