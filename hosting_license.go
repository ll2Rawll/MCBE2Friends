package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const grantMarker = "MCBE2FRIENDS-PREMIUM-1"
const grantBase = "https://raw.githubusercontent.com/ll2Rawll/MCBE2Friends/premium-keys/grants/"

var devicePattern = regexp.MustCompile("^PC-[A-Z2-7]{16}$")
var keyPattern = regexp.MustCompile("^M2F[A-Z2-7]{26}$")

func installationCode(dir string) (string, error) {
	path := filepath.Join(dir, "installation.txt")
	raw, err := os.ReadFile(path)
	if err == nil {
		code := strings.TrimSpace(string(raw))
		if !devicePattern.MatchString(code) {
			return "", errors.New("The saved PC code is damaged. Restore installation.txt from your backup.")
		}
		return code, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var random [10]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	code := "PC-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random[:])
	return code, atomicWrite(path, []byte(code+"\n"))
}

func cleanKey(key string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "", "\r", "", "\t", "").Replace(key))
}
func grantDigest(device, key string) string {
	d := sha256.Sum256([]byte(grantMarker + "\n" + device + "\n" + cleanKey(key)))
	return hex.EncodeToString(d[:])
}
func verifyGrant(ctx context.Context, base, device, key string) error {
	if !devicePattern.MatchString(device) || !keyPattern.MatchString(cleanKey(key)) {
		return errors.New("Enter the full M2F key you received on Discord.")
	}
	url := base + grantDigest(device, key) + ".txt?check=" + fmt.Sprint(time.Now().Unix()/30)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cache-Control", "no-cache")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("Could not reach the key registry. Connect to the internet and retry.")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errors.New("This key has no approval for this PC code. Check both with the seller, then retry.")
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("The key registry returned HTTP %d. Try again shortly.", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil || strings.TrimSpace(string(data)) != grantMarker {
		return errors.New("The approval could not be verified. Ask the seller to check the grant.")
	}
	return nil
}

func (h *Hosting) activate(ctx context.Context, key string) error {
	h.licenseOp.Lock()
	defer h.licenseOp.Unlock()
	if h.device == "" {
		return errors.New("Cannot read the install’s PC code. Check its local data folder.")
	}
	path := filepath.Join(h.root, "premium-key.txt")
	if key == "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return errors.New("Enter your Premium key first.")
		}
		key = string(b)
	}
	if err := verifyGrant(ctx, h.grants, h.device, key); err != nil {
		h.mu.Lock()
		if !h.licensed {
			h.licenseMessage = err.Error()
		}
		h.mu.Unlock()
		return err
	}
	if err := atomicWrite(path, []byte(cleanKey(key)+"\n")); err != nil {
		return err
	}
	h.mu.Lock()
	h.licensed = true
	h.licenseMessage = ""
	h.mu.Unlock()
	return nil
}
