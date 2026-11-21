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

func openBrowser(address string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address)
	case "darwin":
		cmd = exec.Command("open", address)
	default:
		cmd = exec.Command("xdg-open", address)
	}
	// Desktop openers can stay alive until the browser closes.
	if err := cmd.Start(); err != nil {
		return err
	}
	opened := make(chan error, 1)
	go func() { opened <- cmd.Wait() }()
	select {
	case err := <-opened:
		return err
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func present(i Instance, noBrowser bool) error {
	if noBrowser {
		fmt.Println(i.launchURL())
		return nil
	}
	if err := openBrowser(i.launchURL()); err != nil {
		return fmt.Errorf("open this address in your browser to use Mcbe2Friends:\n%s", i.launchURL())
	}
	return nil
}

func reopen(path string, noBrowser bool) error {
	// The first process may still be writing the discovery file.
	client := &http.Client{Timeout: 400 * time.Millisecond, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	for tries := 0; tries < 25; tries++ {
		b, err := os.ReadFile(path)
		var i Instance
		if err == nil && json.Unmarshal(b, &i) == nil && i.Port > 0 && i.Port <= 65535 && len(i.Token) >= 26 {
			req, _ := http.NewRequest(http.MethodGet, i.origin()+"/api/state", nil)
			req.Header.Set("Authorization", "Bearer "+i.Token)
			if resp, err := client.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return present(i, noBrowser)
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("The launcher is already open but did not respond. Close it and open Mcbe2Friends again.")
}

func serve(dataPath string, noBrowser bool) error {
	base, err := os.UserConfigDir()
	if err != nil && dataPath == "" {
		return err
	}
	if runtime.GOOS == "windows" && os.Getenv("LOCALAPPDATA") != "" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if dataPath == "" {
		dataPath = filepath.Join(base, "Mcbe2Friends")
	}
	root, err := filepath.Abs(dataPath)
	if err != nil {
		return err
	}
	privateFiles()
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	discovery := filepath.Join(root, "launcher.json")
	lock, err := lockInstance(filepath.Join(root, "launcher.lock"))
	if errors.Is(err, errInUse) {
		return reopen(discovery, noBrowser)
	}
	if err != nil {
		return err
	}
	defer lock.Close()

	l, err := newLauncher(root)
	if err != nil {
		return err
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
	server := &http.Server{Handler: handler(l, i, cancel), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	if err := present(i, noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, err)
		showMessage(err.Error())
	}
	var result error
	select {
	case <-ctx.Done():
	case result = <-served:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	// Do not release the instance lock while Java is still writing its cache.
	done := l.stop(true)
	shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	server.Shutdown(shutdown)
	<-done
	return result
}

// Finder reopens an existing GUI process instead of launching it again.
// Let the app bundle hand off to a background host so double-clicking the
// app always brings up a fresh browser tab, even after the old tab closed.
func launch(path string, noBrowser bool) error {
	if runtime.GOOS == "darwin" && !noBrowser && os.Getenv("MCBE2FRIENDS_APP_HOST") == "" {
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		if filepath.Base(filepath.Dir(binary)) == "MacOS" && filepath.Base(filepath.Dir(filepath.Dir(binary))) == "Contents" {
			cmd := exec.Command(binary, os.Args[1:]...)
			cmd.Env = append(os.Environ(), "MCBE2FRIENDS_APP_HOST=1")
			if err := cmd.Start(); err != nil {
				return err
			}
			return cmd.Process.Release()
		}
	}
	return serve(path, noBrowser)
}

func main() {
	noBrowser := flag.Bool("no-browser", false, "print the local launcher URL instead of opening a browser")
	dir := flag.String("data-dir", "", "use a separate settings and runtime directory")
	flag.Parse()
	if err := launch(*dir, *noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if !*noBrowser {
			showMessage(err.Error())
		}
		os.Exit(1)
	}
}
