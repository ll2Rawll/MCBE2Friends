package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRememberServicesIndependently(t *testing.T) {
	root := t.TempDir()
	b, err := readBackground(root)
	if err != nil || b.want.any() {
		t.Fatal("existing profiles should start with both services off", err)
	}
	var registrations []bool
	b.register = func(on bool) error { registrations = append(registrations, on); return nil }
	var wg sync.WaitGroup
	for _, service := range []serviceName{bedrockService, broadcastService} {
		wg.Go(func() {
			if err := b.set(service, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	cold := func() services {
		saved, err := readBackground(root)
		if err != nil {
			t.Fatal(err)
		}
		return saved.want
	}
	if cold() != (services{true, true}) {
		t.Fatal(cold())
	}
	if err := b.set(broadcastService, false); err != nil || cold() != (services{Server: true}) {
		t.Fatal(cold(), err)
	}
	if err := b.set(bedrockService, false); err != nil || cold().any() {
		t.Fatal(cold(), err)
	}
	if !slices.Equal(registrations, []bool{true, false}) {
		t.Fatal(registrations)
	}
	b.register = func(bool) error { return errors.New("access denied") }
	if err := b.set(broadcastService, true); err != nil || !cold().Broadcast || b.status().Warning == "" {
		t.Fatal("unreported startup failure", err)
	}
	b.register = func(bool) error { return nil }
	b.retryRegistration()
	if b.status().Warning != "" {
		t.Fatal("could not repair startup registration")
	}
}

func TestClosingControlsPreservesWorkAndStopCancelsRecovery(t *testing.T) {
	root := t.TempDir()
	l, err := newLauncher(root)
	if err != nil {
		t.Fatal(err)
	}
	i := Instance{6000, strings.Repeat("a", 32)}
	exited := false
	api := handler(l, i, func() { exited = true })
	request := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", i.origin()+path, nil)
		req.Header.Set("Authorization", "Bearer "+i.Token)
		api.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	// A pending license check must be cancellable even without a valid key.
	hctx, cancel := context.WithCancel(t.Context())
	l.host.cancelRestore = cancel
	bctx, cancelb := context.WithCancel(t.Context())
	l.cancelRestore = cancelb
	for _, service := range []serviceName{broadcastService, bedrockService} {
		if err := l.background.set(service, true); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(request("/api/quit"), "false") || exited {
		t.Fatal("closed worker with pending services")
	}
	request("/api/hosting/stop")
	if hctx.Err() == nil || bctx.Err() != nil || l.background.status().services != (services{Broadcast: true}) {
		t.Fatal("stop affected the wrong service")
	}
	request("/api/stop")
	if bctx.Err() == nil || l.background.status().any() {
		t.Fatal("broadcast restore survived Stop")
	}
	if !strings.Contains(request("/api/quit"), "true") || !exited {
		t.Fatal("idle worker did not exit")
	}
	if err := serve(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "launcher.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("idle startup opened controls")
	}
}

func TestStopDuringRegistryOutage(t *testing.T) {
	l, err := newLauncher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := l.host
	if !h.hw.Supported {
		t.Skip("hosting is unavailable on this machine")
	}
	registry := fixtureRegistry(t, h)
	registry.Store(503)
	if err := os.WriteFile(filepath.Join(h.root, "premium-key.txt"), []byte(approvalTestKey), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.background.set(bedrockService, true); err != nil {
		t.Fatal(err)
	}
	l.recoverServices(t.Context())
	deadline := time.Now().Add(8 * time.Second)
	for !strings.Contains(h.status().LicenseMessage, "unavailable") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(h.status().LicenseMessage, "unavailable") || h.isBusy() {
		t.Fatal("unverified service ran or did not attempt recovery")
	}
	if _, err := h.stop(false); err != nil {
		t.Fatal(err)
	}
	registry.Store(200)
	deadline = time.Now().Add(2 * time.Second)
	for h.status().Checking && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.isBusy() || h.status().Checking || l.background.status().Server {
		t.Fatal("cancelled verification still scheduled hosting")
	}
}
