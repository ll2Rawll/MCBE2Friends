package main

import (
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Detached GUI applications have no console to receive CTRL_SHUTDOWN_EVENT.
// A hidden top-level window receives the ordinary Windows session-end message.
var (
	sessionUI      = windows.NewLazySystemDLL("user32.dll")
	sessionWindows sync.Map
	sessionProc    = windows.NewCallback(sessionMessage)
)

type sessionWindow struct {
	cancel   func()
	saved    chan struct{}
	previous uintptr
}

func sessionMessage(hwnd, message, wParam, lParam uintptr) uintptr {
	entry, ok := sessionWindows.Load(hwnd)
	if !ok {
		result, _, _ := sessionUI.NewProc("DefWindowProcW").Call(hwnd, message, wParam, lParam)
		return result
	}
	w := entry.(*sessionWindow)
	switch message {
	case 0x0011: // WM_QUERYENDSESSION
		return 1
	case 0x0016: // WM_ENDSESSION
		if wParam != 0 {
			w.cancel()
			<-w.saved
		}
		return 0
	case 0x0002: // WM_DESTROY
		sessionUI.NewProc("PostQuitMessage").Call(0)
	}
	result, _, _ := sessionUI.NewProc("CallWindowProcW").Call(w.previous, hwnd, message, wParam, lParam)
	return result
}

func watchSessionEnd(cancel func()) func() {
	w := &sessionWindow{cancel: cancel, saved: make(chan struct{})}
	ready := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		class, _ := windows.UTF16PtrFromString("STATIC")
		hwnd, _, _ := sessionUI.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
		if hwnd == 0 {
			ready <- 0
			return
		}
		defer sessionWindows.Delete(hwnd)
		sessionWindows.Store(hwnd, w)
		w.previous, _, _ = sessionUI.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(3), sessionProc)
		if w.previous == 0 {
			sessionUI.NewProc("DestroyWindow").Call(hwnd)
			ready <- 0
			return
		}
		ready <- hwnd
		var msg struct {
			hwnd           uintptr
			message        uint32
			wParam, lParam uintptr
			time           uint32
			x, y           int32
			private        uint32
		}
		get := sessionUI.NewProc("GetMessageW")
		dispatch := sessionUI.NewProc("DispatchMessageW")
		for {
			next, _, _ := get.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(next) <= 0 {
				return
			}
			dispatch.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	hwnd := <-ready
	return func() {
		close(w.saved)
		if hwnd != 0 {
			sessionUI.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0)
		}
	}
}
