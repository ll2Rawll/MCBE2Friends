package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type javaStatus struct {
	Phase   string   `json:"phase"`
	Message string   `json:"message"`
	Percent int      `json:"percent"`
	Busy    bool     `json:"busy"`
	Log     []string `json:"log,omitempty"`
}
type javaState struct {
	javaStatus
	active  *hostRun
	config  JavaConfig
	input   io.Writer
	inputMu sync.Mutex
}

func (j *javaState) snapshot(log bool) javaStatus {
	s := j.javaStatus
	s.Busy = j.active != nil
	s.Log = nil
	if log {
		s.Log = slices.Clone(j.Log)
	}
	return s
}
func (j *javaState) cancelLocked() {
	if j.active != nil {
		j.Phase, j.Message = "stopping", "Saving the world and stopping"
		j.active.cancel()
	}
}
func (h *Hosting) javaStateLocked(id string) *javaState {
	j := h.java[id]
	if j == nil {
		j = &javaState{javaStatus: javaStatus{Phase: "offline", Percent: -1}}
		h.java[id] = j
	}
	return j
}
func (h *Hosting) activeJavaLocked() []string {
	var ids []string
	for id, j := range h.java {
		if j.active != nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
func (h *Hosting) startJava(id string, consent bool, restore context.Context) error {
	h.ops.Lock()
	defer h.ops.Unlock()
	if err := h.allowed(false); err != nil {
		return err
	}
	if !h.hw.JavaSupported {
		return errors.New("Java hosting needs a supported computer with at least 4 GB of RAM.")
	}
	dir, err := h.javaServer(id)
	if err != nil {
		return err
	}
	c, err := h.javaConfig(dir)
	if err != nil {
		return err
	}
	if err = c.validate(h.hw); err != nil {
		return err
	}
	if _, err = worldDirectory(dir, c.World); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing || restore != nil && (restore.Err() != nil || !slices.Contains(h.background.status().Java, id)) {
		return errors.New("Java startup was cancelled.")
	}
	if !h.validLicenseLocked(time.Now()) {
		return errors.New("Recheck your Premium key before starting.")
	}
	j := h.javaStateLocked(id)
	if j.active != nil {
		return errors.New("This Java server is already active.")
	}
	memory := c.Memory
	for _, instance := range h.java {
		if instance.active == nil {
			continue
		}
		other := instance.config
		if javaPortsOverlap(c, other) {
			return fmt.Errorf("A port is already reserved by %s. Choose different ports or stop that server.", other.Name)
		}
		memory += other.Memory
	}
	if memory > h.hw.heapBudget() {
		return errors.New("These servers would exceed this PC’s hosting memory budget. Reduce the memory limits or stop another server.")
	}
	if !javaTerms(dir) {
		if !consent {
			return errors.New("Accept the Minecraft EULA before starting this server.")
		}
		if err = atomicWrite(filepath.Join(dir, "eula.txt"), []byte("eula=true\n")); err != nil {
			return err
		}
	}
	if err = h.background.setJava(id, true); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &hostRun{cancel: cancel, done: make(chan struct{})}
	j.active, j.config = run, c
	j.javaStatus = javaStatus{Phase: "installing", Message: "Preparing your Java server", Percent: -1}
	go h.runJava(ctx, run, j, dir, c)
	return nil
}
func javaPortsOverlap(a, b JavaConfig) bool {
	return a.Port == b.Port || a.Geyser && a.BedrockPort == b.Port || b.Geyser && b.BedrockPort == a.Port || a.Geyser && b.Geyser && a.BedrockPort == b.BedrockPort
}
func (h *Hosting) stopJava(id string) error {
	h.ops.Lock()
	defer h.ops.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.background.setJava(id, false); err != nil {
		return err
	}
	if j := h.java[id]; j != nil {
		j.cancelLocked()
	}
	want := h.background.status()
	if h.cancelRestore != nil && !want.Server && len(want.Java) == 0 {
		h.cancelRestore()
	}
	return nil
}
func (h *Hosting) runJava(ctx context.Context, run *hostRun, j *javaState, dir string, c JavaConfig) {
	progress := func(msg string, p int) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if ctx.Err() == nil {
			j.Message, j.Percent = msg, p
		}
	}
	java, err := h.installJava(ctx, dir, c, progress)
	if err == nil && ctx.Err() == nil {
		err = h.launchJava(ctx, j, dir, c, java)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	j.active, j.input, j.Percent = nil, nil, -1
	if ctx.Err() != nil || err == nil {
		j.Phase, j.Message = "offline", "Java server stopped."
	} else {
		j.Phase, j.Message = "error", err.Error()
	}
	run.cancel()
	close(run.done)
}
func javaListenHost(dir string, c JavaConfig) (string, error) {
	p, err := readJavaProperties(dir)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(p.GetString("online-mode", "true"), "true") || !strings.EqualFold(p.GetString("enable-status", "true"), "true") {
		return "", errors.New("Set online-mode and enable-status to true in server.properties before starting.")
	}
	if c.Geyser && p.GetBool("enforce-secure-profile", true) {
		if err = c.write(dir); err != nil {
			return "", err
		}
	}
	host := p.GetString("server-ip", "")
	if host != "" && net.ParseIP(host) == nil {
		return "", errors.New("Set server-ip to a local IP address or leave it blank.")
	}
	return host, nil
}
func javaPortsFree(ctx context.Context, host string, c JavaConfig) error {
	var lc net.ListenConfig
	check := func(ip string, port int, udp bool) error {
		addr := net.JoinHostPort(ip, strconv.Itoa(port))
		tcp, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("TCP port %d is unavailable. Stop its other server or choose another port: %w", port, err)
		}
		defer tcp.Close()
		if udp {
			pc, err := lc.ListenPacket(ctx, "udp", addr)
			if err != nil {
				return fmt.Errorf("Geyser UDP port %d is unavailable: %w", port, err)
			}
			pc.Close()
		}
		return nil
	}
	if err := check(host, c.Port, false); err != nil {
		return err
	}
	if c.Geyser {
		return check("0.0.0.0", c.BedrockPort, true)
	}
	return nil
}

var consoleEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

func (h *Hosting) launchJava(ctx context.Context, j *javaState, dir string, c JavaConfig, java string) error {
	host, err := javaListenHost(dir, c)
	if err != nil {
		return err
	}
	if err = javaPortsFree(ctx, host, c); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(dir, "worlds"), 0700); err != nil {
		return err
	}
	args := javaNetworkOptions(managedJavaRuntime(h, java))
	if runtime.GOOS == "darwin" {
		guard, e := javaLifetimeOption(dir)
		if e != nil {
			return e
		}
		args = append(args, guard)
	}
	args = append(args, "-XX:ActiveProcessorCount="+strconv.Itoa(max(1, min(8, h.hw.Cores))), "-Xms256M", fmt.Sprintf("-Xmx%dM", c.Memory), "-Dterminal.jline=false", "-Dterminal.ansi=false", "-jar", "server.jar", "--universe", "worlds", "--nogui")
	cmd := exec.CommandContext(ctx, java, args...)
	cmd.Dir = dir
	configureChild(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer in.Close()
	cmd.Cancel = func() error {
		j.inputMu.Lock()
		defer j.inputMu.Unlock()
		_, e := io.WriteString(in, "stop\n")
		return e
	}
	cmd.WaitDelay = 45 * time.Second
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	cmd.Stdout, cmd.Stderr = w, w
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("Java could not start: %w", err)
	}
	release, err := bindChildLifetime(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		r.Close()
		_ = cmd.Wait()
		return fmt.Errorf("Could not supervise the Java server: %w", err)
	}
	defer release()
	h.mu.Lock()
	j.input = in
	if ctx.Err() == nil {
		j.Phase, j.Message, j.Percent = "starting", "Loading the Java world", -1
	}
	h.mu.Unlock()
	probeCtx, cancelProbe := context.WithCancel(ctx)
	var tasks sync.WaitGroup
	tasks.Go(func() {
		defer r.Close()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 16384), 2<<20)
		started := false
		for scanner.Scan() {
			line := strings.TrimSpace(consoleEscape.ReplaceAllString(scanner.Text(), ""))
			if line == "" {
				continue
			}
			if len(line) > 1200 {
				line = line[:1200] + "…"
			}
			h.mu.Lock()
			if len(j.Log) >= 80 {
				j.Log = j.Log[1:]
			}
			j.Log = append(j.Log, line)
			h.mu.Unlock()
			if !started && strings.Contains(line, "Done (") && strings.Contains(line, "For help") {
				started = true
				tasks.Go(func() { h.javaReady(probeCtx, j, host, c) })
			}
		}
		_, _ = io.Copy(io.Discard, r)
	})
	err = cmd.Wait()
	cancelProbe()
	w.Close()
	tasks.Wait()
	if err != nil {
		return fmt.Errorf("Java exited (%v). Open the server console for details.", err)
	}
	return nil
}
func (h *Hosting) javaCommand(id, line string) error {
	if err := h.allowed(false); err != nil {
		return err
	}
	line = strings.TrimPrefix(strings.TrimSpace(line), "/")
	if line == "" || len(line) > 500 || strings.IndexFunc(line, unicode.IsControl) >= 0 {
		return errors.New("Enter one Java console command of up to 500 characters.")
	}
	if strings.EqualFold(line, "stop") || strings.EqualFold(line, "minecraft:stop") {
		return h.stopJava(id)
	}
	h.mu.Lock()
	j := h.java[id]
	if j == nil || j.input == nil || j.Phase != "running" && j.Phase != "unreachable" {
		h.mu.Unlock()
		return errors.New("Start this server before sending commands.")
	}
	in := j.input
	h.mu.Unlock()
	j.inputMu.Lock()
	defer j.inputMu.Unlock()
	_, err := io.WriteString(in, line+"\n")
	return err
}
