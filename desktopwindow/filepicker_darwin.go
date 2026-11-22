//go:build darwin && desktop

package desktopwindow

// WKWebView's native file picker uses UTType to apply file filters.
// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
