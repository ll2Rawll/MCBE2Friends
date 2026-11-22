package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bedrock announces startup before some networking failures surface. Ask its
// local NetherNet signaling endpoint for the same world the launcher started.
func (h *Hosting) confirmReady(ctx context.Context, c HostConfig) {
	h.mu.Lock()
	run := h.active
	if h.phase != "starting" || run == nil {
		h.mu.Unlock()
		return
	}
	h.message = "Checking the local Bedrock listener"
	h.mu.Unlock()
	go func() {
		next, msg := "running", "Your Bedrock server is running."
		raw, _ := os.ReadFile(filepath.Join(h.server, "server.properties"))
		props := properties(raw)
		if !strings.EqualFold(props["transport"], "raknet") {
			host := "127.0.0.1"
			if ip := net.ParseIP(props["server-ip"]); ip != nil && !ip.IsUnspecified() {
				host = ip.String()
			}
			probe, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			transport := &http.Transport{DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
			url := "http://" + net.JoinHostPort(host, strconv.Itoa(c.Port)) + "/v1/join"
			for {
				req, _ := http.NewRequestWithContext(probe, "GET", url, nil)
				res, e := client.Do(req)
				if e == nil {
					var info struct {
						Level, Version string
						Protocol       int
					}
					e = json.NewDecoder(io.LimitReader(res.Body, 16384)).Decode(&info)
					res.Body.Close()
					if res.StatusCode == 200 && e == nil && info.Protocol > 0 && info.Version != "" && info.Level == c.World {
						msg = "Bedrock is running. The local world and signaling port are verified."
						break
					}
				}
				select {
				case <-probe.Done():
					next = "unreachable"
					msg = "Bedrock started but its local world did not answer on the configured port. Check the console and server.properties."
				case <-time.After(300 * time.Millisecond):
					continue
				}
				break
			}
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if ctx.Err() == nil && h.active == run && h.phase == "starting" {
			h.phase, h.message = next, msg
		}
	}()
}
