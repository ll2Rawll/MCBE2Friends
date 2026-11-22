package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (h *Hosting) waitForPort(ctx context.Context, c HostConfig) error {
	raw, err := os.ReadFile(filepath.Join(h.server, "server.properties"))
	if err != nil {
		return err
	}
	props := properties(raw)
	if strings.EqualFold(props["transport"], "raknet") {
		return nil
	}
	host := props["server-ip"]
	if host != "" && net.ParseIP(host) == nil {
		return errors.New("Check server-ip in server.properties. It must be empty or a local IP address.")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(c.Port))
	local := host
	if local == "" || net.ParseIP(local).IsUnspecified() {
		local = "127.0.0.1"
	}
	dialAddr := net.JoinHostPort(local, strconv.Itoa(c.Port))
	probe := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var optErr error
		if err := c.Control(func(fd uintptr) { optErr = bedrockPortOptions(fd) }); err != nil {
			return err
		}
		return optErr
	}}
	// Bedrock can report startup without a listener when the OS still owns
	// a previous session's port. Wait before handing that port to Bedrock.
	deadline := time.Now().Add(80 * time.Second)
	for {
		sock, e := probe.Listen(ctx, "tcp", addr)
		if e == nil {
			sock.Close()
			break
		}
		if !isPortInUse(e) {
			return fmt.Errorf("Cannot use TCP port %d: %w", c.Port, e)
		}
		check, e := net.DialTimeout("tcp", dialAddr, 200*time.Millisecond)
		if e == nil {
			check.Close()
			return fmt.Errorf("TCP port %d is in use. Stop the other server or choose a different port in settings.", c.Port)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("TCP port %d is still occupied. Stop the other service or choose another port.", c.Port)
		}
		h.progress("starting", fmt.Sprintf("Waiting for this PC to release TCP port %d after the previous session", c.Port), -1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if port, e := strconv.ParseUint(props["server-udp-ports"], 10, 16); e == nil && port > 0 {
		udp, e := net.ListenPacket("udp", net.JoinHostPort(host, strconv.Itoa(int(port))))
		if e != nil {
			return fmt.Errorf("Cannot use UDP port %d. Check other servers and server.properties: %w", port, e)
		}
		udp.Close()
	}
	return ctx.Err()
}
