// Package desktopwindow presents embedded application assets in a native desktop window.
package desktopwindow

import (
	"context"
	"io/fs"
	"net/http"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Window struct {
	Title, ID, DataPath string
	Width, Height       int
	Assets              fs.FS
	Handler             http.Handler
	Bind                []interface{}
	Icon                []byte
	OnClose             func()
}

func Show(window Window) error {
	// A second launch restores the existing window for this profile. A background
	// worker has no window lock and keeps its independent service lifetime.
	var mu sync.Mutex
	var active context.Context
	return wails.Run(&options.App{
		Title: window.Title, Width: window.Width, Height: window.Height, MinWidth: 380, MinHeight: 480,
		BackgroundColour: options.NewRGBA(23, 39, 36, 255),
		Bind:             window.Bind,
		AssetServer:      &assetserver.Options{Assets: window.Assets, Handler: window.Handler},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: window.ID, OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			mu.Lock()
			ctx := active
			mu.Unlock()
			if ctx != nil {
				runtime.WindowUnminimise(ctx)
				runtime.WindowShow(ctx)
			}
		}},
		OnStartup: func(ctx context.Context) { mu.Lock(); active = ctx; mu.Unlock() },
		OnShutdown: func(ctx context.Context) {
			mu.Lock()
			active = nil
			mu.Unlock()
			if window.OnClose != nil {
				window.OnClose()
			}
		},
		Windows: &windows.Options{Theme: windows.Dark, BackdropType: windows.Mica, WebviewUserDataPath: window.DataPath},
		Mac:     &mac.Options{Appearance: mac.NSAppearanceNameDarkAqua, About: &mac.AboutInfo{Title: window.Title, Icon: window.Icon}},
		Linux:   &linux.Options{Icon: window.Icon, ProgramName: window.Title, WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand},
	})
}
