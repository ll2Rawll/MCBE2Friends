package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const grantMarker = "MCBE2FRIENDS-PREMIUM-2"
const grantBase = "https://raw.githubusercontent.com/ll2Rawll/MCBE2Friends/premium-keys/"
const renewalInterval = 5 * time.Minute
const licenseWindow = 30 * time.Minute

var grantRecord = regexp.MustCompile("^MCBE2FRIENDS-PREMIUM-2 ([0-9a-f]{32})(?:\r?\n)?$")
var registryUnavailable = errors.New("The Premium registry is unavailable. Connect to the internet and try again.")
var registryClient = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}}

func cleanKey(key string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "", "\r", "", "\t", "").Replace(key))
}
func grantDigest(device, key string) string {
	d := sha256.Sum256([]byte(grantMarker + "\n" + device + "\n" + cleanKey(key)))
	return hex.EncodeToString(d[:])
}

func registryFile(ctx context.Context, location string, limit int64, missing string) ([]byte, error) {
	// A newly approved or revoked purchase must not reuse an earlier CDN response.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location+"?check="+rand.Text(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := registryClient.Do(req)
	if err != nil {
		return nil, registryUnavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New(missing)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500:
		return nil, registryUnavailable
	case resp.StatusCode != http.StatusOK:
		return nil, errors.New("GitHub did not confirm this Premium approval.")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, registryUnavailable
	}
	if int64(len(b)) > limit {
		return nil, errors.New("The Premium approval is invalid.")
	}
	return b, nil
}

func verifySubject(ctx context.Context, base, subject string) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	record, err := registryFile(ctx, base+"grants-v2/"+subject+".txt", 128, "This key has no current approval for this PC. Check with the project owner.")
	if err != nil {
		return err
	}
	match := grantRecord.FindSubmatch(record)
	if match == nil {
		return errors.New("This Premium approval has an invalid format.")
	}
	digest := approvalDigest(subject, string(match[1]))
	proof, err := registryFile(ctx, base+"proofs-v2/"+hex.EncodeToString(digest[:])+".json", 128<<10,
		"The signed approval is not available yet. Wait a moment and retry.")
	if err != nil {
		return err
	}
	if err = checkAttestation(proof, digest); err != nil {
		return errors.New("The launcher's trusted signature check did not confirm this purchase.")
	}
	return nil
}

func (h *Hosting) validLicenseLocked(now time.Time) bool {
	return h.subject != "" && now.Before(h.validUntil)
}

func (h *Hosting) scheduleRenewalLocked(delay time.Duration) {
	if h.renewal != nil {
		h.renewal.Stop()
	}
	h.renewal = time.AfterFunc(delay, h.refreshLicense)
}

func (h *Hosting) grantLocked(subject string, now time.Time) {
	h.subject = subject
	h.validUntil = now.Add(licenseWindow)
	h.licenseMessage = ""
	h.scheduleRenewalLocked(renewalInterval)
	if h.expiry != nil {
		h.expiry.Stop()
	}
	h.expiry = time.AfterFunc(licenseWindow, h.expireLicense)
}

// A slow registry request must never extend the last verified interval.
func (h *Hosting) expireLicense() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closing && h.subject != "" && !time.Now().Before(h.validUntil) {
		h.withdrawLocked("Premium verification expired. Recheck your saved key.")
	}
}

func (h *Hosting) withdrawLocked(message string) {
	h.subject = ""
	h.validUntil = time.Time{}
	h.licenseMessage = message
	if h.renewal != nil {
		h.renewal.Stop()
		h.renewal = nil
	}
	if h.expiry != nil {
		h.expiry.Stop()
		h.expiry = nil
	}
	for _, j := range h.java {
		j.cancelLocked()
	}
	if h.active != nil {
		h.phase, h.message = "stopping", "Saving the world and stopping"
		h.active.cancel()
	}
}

// Refresh independently of the browser. Only a temporary outage gets the remainder
// of the last verified interval. A withdrawn or invalid approval takes effect immediately.
func (h *Hosting) refreshLicense() {
	h.licenseOp.Lock()
	defer h.licenseOp.Unlock()
	h.mu.Lock()
	subject, deadline := h.subject, h.validUntil
	if h.closing || subject == "" {
		h.mu.Unlock()
		return
	}
	if !time.Now().Before(deadline) {
		h.withdrawLocked("Premium verification expired. Recheck your saved key.")
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	err := verifySubject(ctx, h.grants, subject)
	cancel()
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing || h.subject != subject || !h.validUntil.Equal(deadline) {
		return
	}
	if !now.Before(deadline) {
		h.withdrawLocked("Premium verification expired. Recheck your saved key.")
		return
	}
	if err == nil {
		h.grantLocked(subject, now)
		return
	}
	if errors.Is(err, registryUnavailable) && now.Before(deadline) {
		h.licenseMessage = "Premium verification is temporarily offline. Connect before " + deadline.Local().Format("15:04 MST") + " to keep hosting."
		h.scheduleRenewalLocked(min(time.Minute, deadline.Sub(now)))
		return
	}
	h.withdrawLocked(err.Error())
}

func (h *Hosting) activate(ctx context.Context, key string) error {
	h.licenseOp.Lock()
	defer h.licenseOp.Unlock()
	if h.device == "" {
		return errors.New("Could not prepare this PC code. Check the system information in Premium.")
	}
	path := filepath.Join(h.root, "premium-key.txt")
	if key == "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return errors.New("Enter your Premium key first.")
		}
		key = string(b)
	}
	key = cleanKey(key)
	var err error
	subject := grantDigest(h.device, key)
	if !devicePattern.MatchString(h.device) || !keyPattern.MatchString(key) {
		err = errors.New("Enter the full M2F2 key for this PC. Contact the owner to transfer an older purchase.")
	} else {
		err = verifySubject(ctx, h.grants, subject)
	}
	if err != nil {
		h.mu.Lock()
		if h.subject == subject && !errors.Is(err, registryUnavailable) {
			h.withdrawLocked(err.Error())
		}
		if !h.validLicenseLocked(time.Now()) {
			h.licenseMessage = err.Error()
		}
		h.mu.Unlock()
		return err
	}
	if err := atomicWrite(path, []byte(key+"\n")); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return errors.New("The launcher is closing.")
	}
	h.grantLocked(subject, time.Now())
	return nil
}
