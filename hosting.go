package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

type hostRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Hosting struct {
	root, server, device, grants   string
	hw                             Hardware
	mu, ops, licenseOp, inputMu    sync.Mutex
	checking, closing              bool
	subject                        string
	validUntil                     time.Time
	renewal                        *time.Timer
	expiry                         *time.Timer
	licenseMessage, phase, message string
	percent                        int
	active                         *hostRun
	input                          io.Writer
	logs                           []string
}
type HostStatus struct {
	Hardware       Hardware   `json:"hardware"`
	Recommended    HostConfig `json:"recommended"`
	Device         string     `json:"device"`
	Licensed       bool       `json:"licensed"`
	Checking       bool       `json:"checking"`
	SavedKey       bool       `json:"savedKey"`
	LicenseMessage string     `json:"licenseMessage"`
	Installed      bool       `json:"installed"`
	Terms          bool       `json:"terms"`
	Phase          string     `json:"phase"`
	Message        string     `json:"message"`
	Percent        int        `json:"percent"`
	Busy           bool       `json:"busy"`
	Version        string     `json:"version"`
	LAN            string     `json:"lan"`
	Log            []string   `json:"log"`
}

func newHosting(root string) *Hosting {
	dir := filepath.Join(root, "hosting")
	h := &Hosting{root: dir, server: filepath.Join(dir, "server"), grants: grantBase, phase: "offline", percent: -1, hw: hardware()}
	if !h.hw.Supported {
		return h
	}
	err := os.MkdirAll(dir, 0700)
	if err == nil {
		h.device, err = installationCode(dir)
	}
	if err != nil {
		h.licenseMessage = "Could not prepare the Premium folder: " + err.Error()
		return h
	}
	if _, err := os.Stat(filepath.Join(dir, "premium-key.txt")); err == nil {
		h.checking = true
		go func() { _ = h.activate(context.Background(), ""); h.mu.Lock(); h.checking = false; h.mu.Unlock() }()
	}
	return h
}
func (h *Hosting) status() HostStatus {
	h.mu.Lock()
	s := HostStatus{Hardware: h.hw, Recommended: h.hw.defaults(), Device: h.device, Licensed: h.validLicenseLocked(time.Now()), Checking: h.checking, LicenseMessage: h.licenseMessage, Phase: h.phase, Message: h.message, Percent: h.percent, Busy: h.active != nil, Version: bedrockVersion}
	if s.Licensed {
		s.Log = slices.Clone(h.logs)
	}
	h.mu.Unlock()
	_, keyErr := os.Stat(filepath.Join(h.root, "premium-key.txt"))
	s.SavedKey = keyErr == nil
	s.Installed = h.installed()
	_, err := os.Stat(filepath.Join(h.root, "terms-v1"))
	s.Terms = err == nil
	if s.Licensed {
		s.LAN = lanAddress()
	}
	return s
}
func (h *Hosting) isBusy() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.active != nil }
func (h *Hosting) allowed(offline bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.validLicenseLocked(time.Now()) {
		return errors.New("Activate Premium on this PC first.")
	}
	if h.closing {
		return errors.New("The launcher is closing.")
	}
	if offline && h.active != nil {
		return errors.New("Stop the server before changing its files.")
	}
	return nil
}
func (h *Hosting) edit(fn func() error) error {
	h.ops.Lock()
	defer h.ops.Unlock()
	if err := h.allowed(true); err != nil {
		return err
	}
	return fn()
}
func (h *Hosting) progress(phase, msg string, p int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phase != "stopping" {
		h.phase, h.message, h.percent = phase, msg, p
	}
}
func (h *Hosting) start(consent bool) error {
	h.ops.Lock()
	defer h.ops.Unlock()
	if err := h.allowed(true); err != nil {
		return err
	}
	if err := supportedNote(h.hw); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(h.root, "terms-v1")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !consent {
			return errors.New("Review and accept the Minecraft server terms before the first install.")
		}
		if err := atomicWrite(filepath.Join(h.root, "terms-v1"), []byte(time.Now().UTC().Format(time.RFC3339))); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &hostRun{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	if !h.validLicenseLocked(time.Now()) {
		h.mu.Unlock()
		cancel()
		return errors.New("Recheck your Premium key before starting.")
	}
	h.active = run
	h.phase = "starting"
	h.message = "Preparing your server"
	h.percent = -1
	h.logs = nil
	h.mu.Unlock()
	go h.run(ctx, run)
	return nil
}
func (h *Hosting) stop(closing bool) <-chan struct{} {
	h.ops.Lock()
	defer h.ops.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if closing {
		h.closing = true
		if h.renewal != nil {
			h.renewal.Stop()
		}
		if h.expiry != nil {
			h.expiry.Stop()
		}
	}
	if h.active == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	h.phase = "stopping"
	h.message = "Saving the world and stopping"
	h.active.cancel()
	return h.active.done
}
func (h *Hosting) run(ctx context.Context, run *hostRun) {
	err := h.install(ctx)
	if err == nil && ctx.Err() == nil {
		err = h.launchServer(ctx)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active = nil
	h.input = nil
	h.percent = -1
	if ctx.Err() != nil || err == nil {
		h.phase = "offline"
		h.message = "Server stopped."
	} else {
		h.phase = "error"
		h.message = err.Error()
	}
	run.cancel()
	close(run.done)
}
func (h *Hosting) consoleLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(line, "Contents of server.properties:") {
		return
	}
	if len(line) > 750 {
		line = line[:750] + "…"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.logs) >= 80 {
		h.logs = h.logs[1:]
	}
	h.logs = append(h.logs, line)
}
func (h *Hosting) launchServer(ctx context.Context) error {
	c, err := h.config()
	if err != nil {
		return err
	}
	if _, err = h.worldPath(c.World); err != nil {
		return err
	}
	if err := h.waitForPort(ctx, c); err != nil {
		return err
	}
	h.progress("starting", "Starting the Bedrock server", -1)
	cmd := exec.CommandContext(ctx, filepath.Join(h.server, bedrockExe()))
	cmd.Dir = h.server
	configureChild(cmd)
	if runtime.GOOS == "linux" {
		lib := h.server
		if existing := os.Getenv("LD_LIBRARY_PATH"); existing != "" {
			lib += ":" + existing
		}
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lib)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer in.Close()
	cmd.Cancel = func() error {
		h.inputMu.Lock()
		defer h.inputMu.Unlock()
		_, e := io.WriteString(in, "stop\n")
		return e
	}
	cmd.WaitDelay = 20 * time.Second
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err = cmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		return fmt.Errorf("Bedrock could not start. Check the operating system and its runtime libraries: %w", err)
	}
	h.mu.Lock()
	h.input = in
	h.mu.Unlock()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer reader.Close()
		s := bufio.NewScanner(reader)
		s.Buffer(make([]byte, 16384), 2<<20)
		for s.Scan() {
			line := s.Text()
			h.consoleLine(line)
			if strings.HasSuffix(strings.TrimSpace(line), "] Server started.") {
				h.confirmReady(ctx, c)
			}
		}
		io.Copy(io.Discard, reader)
	}()
	err = cmd.Wait()
	writer.Close()
	<-readDone
	if err != nil {
		return fmt.Errorf("Bedrock exited (%v). Check the console for port, world, or system-library errors.", err)
	}
	return nil
}
func (h *Hosting) command(line string) error {
	if err := h.allowed(false); err != nil {
		return err
	}
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "/")
	if line == "" || len(line) > 500 || strings.IndexFunc(line, unicode.IsControl) >= 0 {
		return errors.New("Enter one Bedrock console command of up to 500 characters.")
	}
	if strings.EqualFold(line, "stop") {
		h.stop(false)
		return nil
	}
	h.inputMu.Lock()
	defer h.inputMu.Unlock()
	h.mu.Lock()
	in := h.input
	ready := h.phase == "running" || h.phase == "unreachable"
	h.mu.Unlock()
	if !ready || in == nil {
		return errors.New("Start the server before sending console commands.")
	}
	_, err := io.WriteString(in, line+"\n")
	return err
}

type HostFiles struct {
	Config  HostConfig `json:"config"`
	Worlds  []string   `json:"worlds"`
	Packs   []Pack     `json:"packs"`
	Players []string   `json:"allowedPlayers"`
	Backups []string   `json:"backups"`
}

func (h *Hosting) files() (HostFiles, error) {
	h.ops.Lock()
	defer h.ops.Unlock()
	var out HostFiles
	if err := h.allowed(false); err != nil {
		return out, err
	}
	c, err := h.config()
	if err != nil {
		return out, err
	}
	out.Config = c
	if !h.installed() {
		return out, nil
	}
	dir, err := h.worldPath(c.World)
	if err != nil {
		return out, err
	}
	worlds, err := os.ReadDir(filepath.Join(h.server, "worlds"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	for _, w := range worlds {
		if w.IsDir() && worldNameOK(w.Name()) {
			out.Worlds = append(out.Worlds, w.Name())
		}
	}
	out.Packs, err = h.packs(dir)
	if err != nil {
		return out, err
	}
	var players []struct {
		Name string `json:"name"`
	}
	if b, e := os.ReadFile(filepath.Join(h.server, "allowlist.json")); e == nil && json.Unmarshal(b, &players) == nil {
		for _, p := range players {
			out.Players = append(out.Players, p.Name)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(h.root, "backups"))
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".mcworld") {
			out.Backups = append(out.Backups, e.Name())
		}
	}
	return out, nil
}
func (h *Hosting) openFolder(backups bool) error {
	if err := h.allowed(false); err != nil {
		return err
	}
	if !h.installed() {
		return errors.New("Start the first server install to create its folders.")
	}
	dir := h.server
	if backups {
		dir = filepath.Join(h.root, "backups")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Open this folder in your file manager: %s", dir)
	}
	go cmd.Wait()
	return nil
}
