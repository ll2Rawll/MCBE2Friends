package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

var errInUse = errors.New("launcher already open")

type Instance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func (i Instance) origin() string    { return "http://127.0.0.1:" + strconv.Itoa(i.Port) }
func (i Instance) launchURL() string { return i.origin() + "/#" + i.Token }

func present(root string, i Instance, noBrowser bool) error {
	if noBrowser {
		fmt.Println(i.launchURL())
		return nil
	}
	return showDesktop(root, i)
}

func liveInstance(client *http.Client, discovery string) (Instance, bool) {
	var i Instance
	b, err := os.ReadFile(discovery)
	if err != nil || json.Unmarshal(b, &i) != nil || i.Port < 1 || i.Port > 65535 || len(i.Token) < 26 {
		return i, false
	}
	req, _ := http.NewRequest(http.MethodGet, i.origin()+"/api/state", nil)
	req.Header.Set("Authorization", "Bearer "+i.Token)
	resp, err := client.Do(req)
	if err != nil {
		return i, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return i, resp.StatusCode == http.StatusOK
}

func openControls(root string, noBrowser bool) error {
	client := &http.Client{Timeout: 450 * time.Millisecond, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	discovery := filepath.Join(root, "launcher.json")
	if i, ok := liveInstance(client, discovery); ok {
		return present(root, i, noBrowser)
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(binary, "-background", "-data-dir", root)
	cmd.Dir = root
	detachWorker(cmd)
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	for deadline := time.Now().Add(18 * time.Second); time.Now().Before(deadline); {
		if i, ok := liveInstance(client, discovery); ok {
			return present(root, i, noBrowser)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("The background launcher did not respond. Run Mcbe2Friends with -background in a terminal to see the startup error.")
}

func profileRoot(dataPath string) (string, error) {
	if dataPath == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" {
			base = os.Getenv("LOCALAPPDATA")
		}
		dataPath = filepath.Join(base, "Mcbe2Friends")
	}
	root, err := filepath.Abs(dataPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func serve(root string, resumeOnly bool) error {
	discovery := filepath.Join(root, "launcher.json")
	lockPath := filepath.Join(root, "launcher.lock")
	lock, err := lockInstance(lockPath)
	if errors.Is(err, errInUse) && !resumeOnly {
		// A simultaneous idle startup or an exiting worker may briefly own
		// the lock. Either attach the opener to a healthy peer or take over.
		client := &http.Client{Timeout: 450 * time.Millisecond, Transport: &http.Transport{}}
		defer client.CloseIdleConnections()
		for deadline := time.Now().Add(16 * time.Second); errors.Is(err, errInUse) && time.Now().Before(deadline); {
			if _, ok := liveInstance(client, discovery); ok {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			lock, err = lockInstance(lockPath)
		}
	}
	if errors.Is(err, errInUse) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	if resumeOnly {
		bg, err := readBackground(root)
		if err != nil {
			return err
		}
		if !bg.want.any() {
			return nil
		}
	}
	l, err := newLauncher(root)
	if err != nil {
		return err
	}
	l.background.register = func(enable bool) error { return registerStartup(root, enable) }
	if !resumeOnly && l.background.want.any() {
		l.background.retryRegistration()
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	i := Instance{Port: listener.Addr().(*net.TCPAddr).Port, Token: rand.Text()}
	info, _ := json.Marshal(i)
	if err := atomicWrite(discovery, info); err != nil {
		return err
	}
	defer os.Remove(discovery)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	finishSession := watchSessionEnd(cancel)
	defer finishSession()
	server := &http.Server{Handler: handler(l, i, cancel), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	served := make(chan error, 1)
	l.recoverServices(ctx)
	go func() { served <- server.Serve(listener) }()
	var result error
	select {
	case <-ctx.Done():
	case result = <-served:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	// Do not release the instance lock while Java is still writing its cache.
	cancel()
	done, _ := l.stop(true)
	hostDone, _ := l.host.stop(true)
	shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	server.Shutdown(shutdown)
	<-done
	<-hostDone
	return result
}

func main() {
	noBrowser := flag.Bool("no-browser", false, "print the local controls URL for headless use")
	windowOnly := flag.Bool("desktop-window", false, "attach the desktop window to an existing worker")
	worker := flag.Bool("background", false, "run the background worker without opening the controls")
	resume := flag.Bool("resume", false, "restore previously enabled services without opening the controls")
	dir := flag.String("data-dir", "", "use a separate settings and runtime directory")
	flag.Parse()
	privateFiles()
	root, err := profileRoot(*dir)
	if err == nil {
		if *worker || *resume {
			err = serve(root, *resume)
		} else if *windowOnly {
			client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
			if i, ok := liveInstance(client, filepath.Join(root, "launcher.json")); ok {
				err = showDesktop(root, i)
			} else {
				err = errors.New("The background service is unavailable. Reopen Mcbe2Friends to reconnect.")
			}
		} else {
			err = openControls(root, *noBrowser)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !*worker && !*resume && !*noBrowser {
			showMessage(err.Error())
		}
		os.Exit(1)
	}
}
