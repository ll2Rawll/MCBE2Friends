package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Use the game's Server List Ping so a different TCP listener cannot be shown as a running world.
func javaPing(ctx context.Context, host string, port int) error {
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "::1"
	}
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	handshake := []byte{0, 0xff, 0xff, 0xff, 0xff, 0x0f}
	handshake = binary.AppendUvarint(handshake, uint64(len(host)))
	handshake = append(handshake, host...)
	handshake = binary.BigEndian.AppendUint16(handshake, uint16(port))
	handshake = append(handshake, 1)
	request := binary.AppendUvarint(nil, uint64(len(handshake)))
	request = append(request, handshake...)
	request = append(request, 1, 0)
	if _, err = conn.Write(request); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	n, err := binary.ReadUvarint(r)
	if err != nil || n < 3 || n > 1<<20 {
		return errors.New("invalid Java status packet")
	}
	packet := make([]byte, n)
	if _, err = io.ReadFull(r, packet); err != nil {
		return err
	}
	data := bytes.NewReader(packet)
	id, err := binary.ReadUvarint(data)
	if err != nil || id != 0 {
		return errors.New("invalid Java status response")
	}
	length, err := binary.ReadUvarint(data)
	if err != nil || length != uint64(data.Len()) {
		return errors.New("invalid Java status length")
	}
	var status struct {
		Version struct {
			Name     string
			Protocol int
		}
		Players *struct{ Max int }
	}
	if err = json.NewDecoder(data).Decode(&status); err != nil {
		return err
	}
	if status.Version.Name == "" || status.Version.Protocol <= 0 || status.Players == nil {
		return errors.New("Java did not return a game version and player status")
	}
	return nil
}
func geyserPing(ctx context.Context, port int) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(port))+"/v1/join", nil)
	if err != nil {
		return err
	}
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var info struct {
		Protocol int
		Version  string
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 16384)).Decode(&info); err != nil {
		return err
	}
	if res.StatusCode != 200 || info.Protocol <= 0 || info.Version == "" {
		return errors.New("Geyser did not return Bedrock join information")
	}
	return nil
}
func (h *Hosting) javaReady(ctx context.Context, j *javaState, host string, c JavaConfig) {
	probe, stop := context.WithTimeout(ctx, 45*time.Second)
	defer stop()
	message := "The Java server answered a Minecraft status request."
	phase := "running"
	for {
		err := javaPing(probe, host, c.Port)
		target := fmt.Sprintf("Java TCP %d", c.Port)
		if err == nil && c.Geyser {
			err = geyserPing(probe, c.BedrockPort)
			target = fmt.Sprintf("Geyser port %d", c.BedrockPort)
			message = "Java and Geyser both answered on their local join ports."
		}
		if err == nil {
			break
		}
		if !retryAfter(probe, 500*time.Millisecond) {
			phase = "unreachable"
			message = target + " did not answer. Check the server console and network settings."
			break
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() == nil && j.Phase == "starting" {
		j.Phase, j.Message = phase, message
	}
}
