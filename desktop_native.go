//go:build desktop

package main

import (
	"io/fs"
	"net/http"
	"path/filepath"

	"mcbe2friends/desktopwindow"
)

func showDesktop(root string, instance Instance) error {
	config, closeRelay, err := launcherWindow(root, instance)
	if err != nil {
		return err
	}
	defer closeRelay()
	if err := desktopwindow.Show(config); err != nil {
		closeDesktopControls(instance)
		return err
	}
	return nil
}

func launcherWindow(root string, instance Instance) (desktopwindow.Window, func(), error) {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return desktopwindow.Window{}, nil, err
	}
	icon, err := fs.ReadFile(assets, "assets/app-icon.png")
	if err != nil {
		return desktopwindow.Window{}, nil, err
	}
	connection, closeRelay, err := startWindowConnection(instance)
	if err != nil {
		return desktopwindow.Window{}, nil, err
	}
	bootstrap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/desktop-connection.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, connection)
	})
	return desktopwindow.Window{
		Title: "Mcbe2Friends", ID: "mcbe2friends-" + profileID(root), DataPath: filepath.Join(root, "desktop-webview"),
		Width: 1200, Height: 820, Assets: assets, Icon: icon, Handler: bootstrap,
		OnClose: func() { closeDesktopControls(instance) },
	}, closeRelay, nil
}
