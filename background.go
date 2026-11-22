package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Persist player intent separately from process status. OS shutdown can kill a
// child before it signals the worker, so a child's exit must never clear it.
type services struct {
	Broadcast bool     `json:"broadcast"`
	Server    bool     `json:"server"`
	Java      []string `json:"java,omitempty"`
}

func (s services) any() bool { return s.Broadcast || s.Server || len(s.Java) != 0 }

type serviceName int

const (
	broadcastService serviceName = iota
	bedrockService
)

type background struct {
	mu       sync.Mutex
	root     string
	want     services
	warning  string
	register func(bool) error
}

func readBackground(root string) (*background, error) {
	b := &background{root: root}
	data, err := os.ReadFile(filepath.Join(root, "resume.json"))
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &b.want)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read background settings: %w", err)
	}
	return b, nil
}

type backgroundStatus struct {
	services
	Warning string `json:"warning,omitempty"`
}

func (b *background) status() backgroundStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.want
	s.Java = slices.Clone(s.Java)
	return backgroundStatus{s, b.warning}
}

// Called under the relevant service lock before launching or cancelling it.
// Failed OS registration still permits play and is reported in the controls.
func (b *background) set(service serviceName, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.want
	if service == bedrockService {
		next.Server = on
	} else {
		next.Broadcast = on
	}
	return b.saveLocked(next)
}

func (b *background) setJava(id string, on bool) error {
	if !javaID.MatchString(id) {
		return errors.New("Choose a Java server.")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.want
	next.Java = slices.Clone(next.Java)
	i := slices.Index(next.Java, id)
	if on && i < 0 {
		next.Java = append(next.Java, id)
		slices.Sort(next.Java)
	}
	if !on && i >= 0 {
		next.Java = slices.Delete(next.Java, i, i+1)
	}
	return b.saveLocked(next)
}

func (b *background) saveLocked(next services) error {
	if next.Server != b.want.Server || next.Broadcast != b.want.Broadcast || !slices.Equal(next.Java, b.want.Java) {
		data, err := json.Marshal(next)
		if err == nil {
			err = atomicWrite(filepath.Join(b.root, "resume.json"), data)
		}
		if err != nil {
			return fmt.Errorf("could not save the startup choice: %w", err)
		}
	}
	update := next.any() != b.want.any() || b.warning != ""
	b.want = next
	if update {
		b.registerLocked()
	}
	return nil
}

func (b *background) registerLocked() {
	if b.register == nil {
		return
	}
	b.warning = ""
	if err := b.register(b.want.any()); err != nil {
		b.warning = "Could not update your operating system’s startup entry. Check your startup permissions and choose Retry startup. " + err.Error()
	}
}

func (b *background) retryRegistration() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.registerLocked()
}

// Portable apps can move after extraction. Point the OS at a private copy of
// the same executable. Only the worker holding the profile lock updates it.
func startupExecutable(root string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "background")
	name := "Mcbe2Friends"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	if target == exe {
		return target, nil
	}
	if old, err := os.ReadFile(target); err == nil && bytes.Equal(old, data) {
		return target, nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = atomicWriteMode(target, data, 0700); err != nil {
		return "", err
	}
	return target, nil
}

func profileID(root string) string {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	id := sha256.Sum256([]byte(root))
	return fmt.Sprintf("Mcbe2Friends-%x", id[:6])
}

func retryAfter(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Start recovery before opening the control socket to requests. Manual Start
// and Stop cancel any outstanding recovery for that service under its lock.
func (l *Launcher) recoverServices(ctx context.Context) {
	want := l.background.status()
	if want.Broadcast {
		var restore context.Context
		restore, l.cancelRestore = context.WithCancel(ctx)
		go l.restoreBroadcast(restore)
	}
	h := l.host
	if !h.hw.Supported && !h.hw.JavaSupported {
		return
	}
	_, err := os.Stat(filepath.Join(h.root, "premium-key.txt"))
	if err != nil && !want.Server && len(want.Java) == 0 {
		return
	}
	restore, cancel := context.WithCancel(ctx)
	h.cancelRestore = cancel
	h.checking = true
	go h.restoreHosting(restore)
}

func (l *Launcher) restoreBroadcast(ctx context.Context) {
	for delay := 5 * time.Second; ctx.Err() == nil; delay = min(2*delay, 2*time.Minute) {
		a, err := l.beginBroadcast(ctx)
		if err != nil {
			if ctx.Err() == nil {
				l.mu.Lock()
				l.event("warning", err.Error())
				l.mu.Unlock()
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-a.done:
		}
		// A fresh Microsoft device code requires the player. Don't keep
		// issuing codes if authorization expired while the panel was closed.
		if a.needsSignIn || !retryAfter(ctx, delay) {
			return
		}
	}
}

func (h *Hosting) restoreHosting(ctx context.Context) {
	defer func() { h.mu.Lock(); h.checking = false; h.mu.Unlock() }()
	for delay := 5 * time.Second; ctx.Err() == nil; delay = min(2*delay, 2*time.Minute) {
		err := h.activate(ctx, "")
		want := h.background.status()
		if err == nil {
			if want.Server && h.hw.Supported {
				if e := h.beginServer(false, ctx); e != nil && ctx.Err() == nil {
					h.mu.Lock()
					if h.active == nil && !h.closing {
						h.phase, h.message = "error", e.Error()
					}
					h.mu.Unlock()
				}
			}
			for _, id := range want.Java {
				if e := h.startJava(id, false, ctx); e != nil && ctx.Err() == nil {
					h.mu.Lock()
					if j := h.javaStateLocked(id); j.active == nil && slices.Contains(h.background.status().Java, id) {
						j.Phase, j.Message = "error", e.Error()
					}
					h.mu.Unlock()
				}
			}
			return
		}
		if (!want.Server && len(want.Java) == 0) || !errors.Is(err, registryUnavailable) || !retryAfter(ctx, delay) {
			return
		}
	}
}

func (l *Launcher) closeControls() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.host.mu.Lock()
	defer l.host.mu.Unlock()
	if l.active != nil || l.host.active != nil || len(l.host.activeJavaLocked()) > 0 || l.background.status().any() {
		return false
	}
	l.closing, l.host.closing = true, true
	return true
}
