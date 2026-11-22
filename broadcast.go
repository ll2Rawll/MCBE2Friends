package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

type State struct {
	Settings      Settings         `json:"settings"`
	Phase         string           `json:"phase"`
	Message       string           `json:"message"`
	Percent       int              `json:"percent"`
	Running       bool             `json:"running"`
	AccountSaved  bool             `json:"accountSaved"`
	Gamertag      string           `json:"gamertag"`
	Code          string           `json:"code,omitempty"`
	SignInURL     string           `json:"signInURL,omitempty"`
	ServerWarning string           `json:"serverWarning,omitempty"`
	Events        []Event          `json:"events"`
	Build         string           `json:"build"`
	Background    backgroundStatus `json:"background"`
	HostBusy      bool             `json:"hostBusy"`
}

type attempt struct {
	cancel      context.CancelFunc
	done        chan struct{}
	failure     string
	needsSignIn bool
}

type Launcher struct {
	host          *Hosting
	background    *background
	cancelRestore context.CancelFunc
	root          string
	mu            sync.Mutex
	state         State
	active        *attempt
	closing       bool
}

func newLauncher(root string) (*Launcher, error) {
	settings, err := readSettings(root)
	if err != nil {
		return nil, fmt.Errorf("could not read your saved server details: %w", err)
	}
	bg, err := readBackground(root)
	if err != nil {
		return nil, err
	}
	l := &Launcher{root: root, background: bg, host: newHosting(root, bg), state: State{Settings: settings, Phase: "offline", Percent: -1, Build: buildNumber}}
	if l.accountSaved() {
		if b, err := os.ReadFile(filepath.Join(root, "gamertag.txt")); err == nil {
			l.state.Gamertag = string(b)
		}
	}
	l.event("info", "Launcher opened.")
	return l, nil
}

func (l *Launcher) accountSaved() bool {
	info, err := os.Stat(filepath.Join(l.root, "broadcast", "cache", "cache.json"))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (l *Launcher) snapshot() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state
	s.Background = l.background.status()
	s.Events = append([]Event{}, s.Events...)
	s.Running = l.active != nil
	s.AccountSaved = l.accountSaved()
	s.HostBusy = l.host.isBusy()
	return s
}

// Call while holding mu.
func (l *Launcher) event(kind, text string) {
	e := Event{time.Now(), kind, text}
	if len(l.state.Events) >= 150 {
		l.state.Events = l.state.Events[1:]
	}
	l.state.Events = append(l.state.Events, e)
}

func (l *Launcher) save(s Settings) error {
	s, err := s.normalized()
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil || l.closing {
		return errors.New("Stop the broadcast before editing server details.")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(l.root, "settings.json"), b); err != nil {
		return err
	}
	l.state.Settings = s
	l.state.Phase, l.state.Message, l.state.ServerWarning = "offline", "", ""
	l.event("success", "Server details saved.")
	return nil
}

func (l *Launcher) forgetAccount() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil || l.closing {
		return errors.New("Stop the broadcast before disconnecting the account.")
	}
	if err := l.background.set(broadcastService, false); err != nil {
		return err
	}
	if l.cancelRestore != nil {
		l.cancelRestore()
	}
	for _, path := range []string{filepath.Join(l.root, "broadcast", "cache", "cache.json"), filepath.Join(l.root, "gamertag.txt")} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	l.state.Gamertag, l.state.Code, l.state.SignInURL = "", "", ""
	l.state.Phase, l.state.Message = "offline", ""
	l.event("info", "Saved Xbox sign-in removed from this computer.")
	return nil
}

func (l *Launcher) start() error {
	_, err := l.beginBroadcast(nil)
	return err
}

func (l *Launcher) beginBroadcast(restore context.Context) (*attempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if restore != nil && restore.Err() != nil {
		return nil, restore.Err()
	}
	if l.active != nil || l.closing {
		return nil, errors.New("The launcher is already working on a broadcast.")
	}
	if _, err := l.state.Settings.normalized(); err != nil {
		return nil, err
	}
	if err := l.background.set(broadcastService, true); err != nil {
		return nil, err
	}
	if restore == nil && l.cancelRestore != nil {
		l.cancelRestore()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &attempt{cancel: cancel, done: make(chan struct{})}
	l.active = a
	l.state.Phase, l.state.Message, l.state.Percent = "preparing", "Preparing the broadcaster", -1
	l.state.Code, l.state.SignInURL, l.state.ServerWarning = "", "", ""
	l.event("info", "Broadcast setup started.")
	go l.run(ctx, a, l.state.Settings)
	return a, nil
}

func (l *Launcher) stop(quitting bool) (<-chan struct{}, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if quitting {
		l.closing = true
	} else if err := l.background.set(broadcastService, false); err != nil {
		return nil, err
	}
	if l.cancelRestore != nil {
		l.cancelRestore()
	}
	if l.active == nil {
		done := make(chan struct{})
		close(done)
		return done, nil
	}
	l.state.Phase, l.state.Message = "stopping", "Stopping the broadcaster"
	l.state.Code, l.state.SignInURL = "", ""
	l.active.cancel()
	return l.active.done, nil
}

func (l *Launcher) progress(a *attempt, text string, percent int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if a != l.active || l.state.Phase == "stopping" {
		return
	}
	if text != l.state.Message {
		l.event("info", text+".")
	}
	l.state.Message, l.state.Percent = text, percent
}

func (l *Launcher) run(ctx context.Context, a *attempt, s Settings) {
	err := l.execute(ctx, a, s)
	l.mu.Lock()
	defer l.mu.Unlock()
	defer close(a.done)
	l.active = nil
	l.state.Code, l.state.SignInURL, l.state.Percent = "", "", -1
	switch {
	case a.failure != "":
		l.state.Phase, l.state.Message = "error", a.failure
		l.event("error", a.failure)
	case ctx.Err() != nil:
		l.state.Phase, l.state.Message = "offline", ""
		l.event("info", "Broadcaster stopped.")
	default:
		l.state.Phase = "error"
		l.state.Message = "MCXboxBroadcast exited before the broadcast was stopped. Start it again to reconnect."
		if err != nil {
			l.state.Message = err.Error()
		}
		l.event("error", l.state.Message)
	}
	a.cancel()
}

const logConfig = `<?xml version="1.0" encoding="UTF-8"?>
<Configuration status="ERROR">
  <Appenders><Console name="Console"><PatternLayout pattern="%msg%n%ex{short}"/></Console></Appenders>
  <Loggers><Root level="info"><AppenderRef ref="Console"/></Root></Loggers>
</Configuration>`

// Java's HTTP clients do not automatically read the proxy environment used
// by the downloader. Only pass the proxy endpoint, never its credentials.
func javaNetworkOptions(managed bool) []string {
	args := []string{"-Djava.net.useSystemProxies=true"}
	// A private Temurin runtime has its own CA bundle. On Linux, prefer
	// the distribution's Java trust store when one is installed so that
	// locally trusted HTTPS proxies work just as they do in the browser.
	if managed && runtime.GOOS == "linux" {
		for _, store := range []string{"/etc/ssl/certs/java/cacerts", "/etc/pki/java/cacerts"} {
			if info, err := os.Stat(store); err == nil && info.Mode().IsRegular() {
				args = append(args, "-Djavax.net.ssl.trustStore="+store)
				break
			}
		}
	}
	for _, scheme := range []string{"http", "https"} {
		u, _ := url.Parse(scheme + "://login.live.com")
		p, err := http.ProxyFromEnvironment(&http.Request{URL: u})
		if err == nil && p != nil && p.Scheme == "http" && p.Hostname() != "" {
			port := p.Port()
			if port == "" {
				port = "80"
			}
			args = append(args, "-D"+scheme+".proxyHost="+p.Hostname(), "-D"+scheme+".proxyPort="+port)
		}
	}
	return args
}

func (l *Launcher) execute(ctx context.Context, a *attempt, s Settings) error {
	java, jar, err := prepare(ctx, l.root, func(msg string, p int) { l.progress(a, msg, p) })
	if err != nil {
		return err
	}
	dir := filepath.Join(l.root, "broadcast")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, "config.yml"), s.yaml()); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, "launcher-log4j2.xml"), []byte(logConfig)); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	managed := strings.HasPrefix(java, filepath.Join(l.root, "components")+string(os.PathSeparator))
	args := append(javaNetworkOptions(managed), "-Dterminal.jline=false", "-Dterminal.ansi=false", "-Dlog4j.configurationFile=launcher-log4j2.xml", "-jar", jar)
	cmd := exec.CommandContext(ctx, java, args...)
	cmd.Dir = dir
	configureChild(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer in.Close()
	cmd.Cancel = func() error {
		_, err := io.WriteString(in, "exit\n")
		return err
	}
	// Upstream starts reading its console only after sign-in. Cancellation
	// before that point needs the deadline to terminate the child.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	l.mu.Lock()
	if ctx.Err() != nil {
		l.mu.Unlock()
		return ctx.Err()
	}
	l.state.Phase, l.state.Message, l.state.Percent = "connecting", "Starting MCXboxBroadcast", -1
	l.event("info", "Starting MCXboxBroadcast build "+buildNumber+".")
	l.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Java could not start the broadcaster: %w", err)
	}
	releaseProcess, err := bindChildLifetime(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("could not attach the broadcaster to its background worker: %w", err)
	}
	defer releaseProcess()
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 16*1024), 2*1024*1024)
	for scanner.Scan() {
		l.handleLine(a, scanner.Text())
	}
	if scanner.Err() != nil {
		// Drain a too-long line so the child's pipe cannot block shutdown.
		a.cancel()
		_, _ = io.Copy(io.Discard, out)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("MCXboxBroadcast stopped (%v). Check the server details and try again.", err)
	}
	return nil
}

var (
	signInLine  = regexp.MustCompile(`^\[Auth\] To sign in, use a web browser to open the page (https://\S+) and enter the code ([A-Za-z0-9-]{4,20}) to authenticate\.$`)
	accountLine = regexp.MustCompile(`^\[Primary Session\] Successfully authenticated as (.{1,80}) \([0-9]+\) with `)
)

func officialSignIn(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "www.microsoft.com", "microsoft.com":
		return u.Path == "/link" || u.Path == "/devicelogin"
	case "login.microsoftonline.com":
		return u.Path == "/common/oauth2/deviceauth"
	}
	return false
}

// Expose only recognized events. Upstream exception bodies can contain
// identifiers or credentials and never belong in the UI or its activity log.
func (l *Launcher) handleLine(a *attempt, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if a != l.active || a.failure != "" || l.state.Phase == "stopping" {
		return
	}
	s := &l.state
	fail := func(text string) {
		a.failure = text
		s.Code, s.SignInURL = "", ""
		a.cancel()
	}
	switch {
	case signInLine.MatchString(line):
		m := signInLine.FindStringSubmatch(line)
		if !officialSignIn(m[1]) {
			fail("Microsoft returned an unfamiliar sign-in page. Check the MCXboxBroadcast release before trying again.")
			return
		}
		a.needsSignIn = true
		s.Phase, s.Message, s.Code, s.SignInURL, s.Gamertag = "signin", "Waiting for Microsoft sign-in", m[2], m[1], ""
		l.event("info", "Microsoft sign-in code received.")
	case accountLine.MatchString(line):
		a.needsSignIn = false
		s.Gamertag = accountLine.FindStringSubmatch(line)[1]
		s.Code, s.SignInURL, s.Phase, s.Message = "", "", "connecting", "Creating the Xbox session"
		if err := atomicWrite(filepath.Join(l.root, "gamertag.txt"), []byte(s.Gamertag)); err != nil {
			l.event("warning", "Could not save the gamertag for the next launch.")
		}
		l.event("success", "Xbox account signed in.")
	case line == "[Primary Session] Creation of Xbox LIVE session was successful!", line == "[Primary Session] WebSocket session reconnected":
		s.Phase, s.Message, s.Code, s.SignInURL = "live", "Xbox session is live", "", ""
		l.event("success", "Xbox Live confirmed the broadcast session.")
	case line == "[Primary Session] Connection to websocket lost, re-creating session...", strings.HasPrefix(line, "[Primary Session] Failed to update session"):
		if s.Phase != "reconnecting" {
			l.event("warning", "Connection interrupted. MCXboxBroadcast is reconnecting.")
		}
		s.Phase, s.Message = "reconnecting", "Reconnecting to Xbox Live"
	case line == "[Primary Session] Updated session!" && s.Phase == "reconnecting":
		s.Phase, s.Message = "live", "Xbox session is live"
		l.event("success", "Xbox session updated successfully.")
	case strings.HasPrefix(line, "[Primary Session] Failed to ping server"):
		if s.ServerWarning == "" {
			s.ServerWarning = "The server did not answer its status query. Check the address and port if friends have trouble joining."
			l.event("warning", s.ServerWarning)
		}
	case strings.Contains(line, "requiring age verification"):
		fail("Xbox needs age verification. Sign in at xbox.com, review the account prompt, then try again.")
	case strings.Contains(line, "expired_token"), strings.Contains(line, "code has expired"):
		fail("The Microsoft sign-in code expired. Try again to get a new code.")
	case strings.Contains(line, "Failed to get/refresh auth token"), strings.Contains(line, "Re-auth still failed with invalid_grant"):
		fail("Microsoft sign-in did not complete. Check your connection and use an account with an Xbox profile, then try again.")
	case strings.Contains(line, "Failed to start NetherNet signaling"), strings.Contains(line, "Session is dead and hit exception"), strings.Contains(line, "Failed to restart session"):
		fail("MCXboxBroadcast could not connect the session. Check your connection and start the broadcast again.")
	case strings.Contains(line, "Failed to load config"):
		fail("MCXboxBroadcast could not read the server settings. Edit the server details and try again.")
	case strings.HasPrefix(line, "Exception in thread \"main\""):
		fail("MCXboxBroadcast could not finish starting. Check your connection and Xbox profile, then try again.")
	}
}
