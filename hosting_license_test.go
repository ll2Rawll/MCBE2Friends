package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const approvalTestPC = "PC2-AAAAAAAAAAAAAAAAAAAAAAAAAA"
const approvalTestKey = "M2F2-AAAAAAAAAAAAAAAAAAAAAAAAAA"
const approvalTestNonce = "934de5f0931f1186b73a7febde4fee8a"

// This synthetic identity's approval was removed from the live registry.
// Use its genuine, archived signature only with an isolated test registry.
func fixtureRegistry(t *testing.T, h *Hosting) *atomic.Int32 {
	t.Helper()
	h.device = approvalTestPC
	s := grantDigest(h.device, approvalTestKey)
	digest := approvalDigest(s, approvalTestNonce)
	proof, err := os.ReadFile("testdata/hosting-attestation.json")
	if err != nil {
		t.Fatal(err)
	}
	status := new(atomic.Int32)
	status.Store(200)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/grants-v2/" + s + ".txt":
			w.WriteHeader(int(status.Load()))
			w.Write([]byte(grantMarker + " " + approvalTestNonce + "\n"))
		case "/proofs-v2/" + hex.EncodeToString(digest[:]) + ".json":
			w.Write(proof)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(registry.Close)
	t.Cleanup(func() { <-h.stop(true) })
	h.grants = registry.URL + "/"
	return status
}

func TestSignedPurchase(t *testing.T) {
	h := newHosting(t.TempDir())
	code := fixtureRegistry(t, h)
	if h.allowed(true) == nil {
		t.Fatal("unexpected access before activation")
	}
	if err := h.activate(t.Context(), "M2F2-BBBBBBBBBBBBBBBBBBBBBBBBBB"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := h.activate(t.Context(), approvalTestKey); err != nil {
		t.Fatal(err)
	}
	if err := h.allowed(true); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(h.status())
	if bytes.Contains(raw, []byte(cleanKey(approvalTestKey))) {
		t.Fatal("private key returned in status")
	}
	if err := verifySubject(t.Context(), h.grants, grantDigest("PC2-BBBBBBBBBBBBBBBBBBBBBBBBBB", approvalTestKey)); err == nil {
		t.Fatal("wrong installation accepted")
	}
	before := h.validUntil
	code.Store(503)
	h.refreshLicense()
	if !h.status().Licensed || !h.validUntil.Equal(before) {
		t.Fatal("outage changed the verified time limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &hostRun{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	h.active = run
	h.mu.Unlock()
	defer func() { cancel(); h.mu.Lock(); h.active = nil; h.mu.Unlock(); close(run.done) }()
	code.Store(404)
	h.refreshLicense()
	if h.status().Licensed || h.allowed(false) == nil || ctx.Err() == nil {
		t.Fatal("withdrawn approval retained access or kept server running")
	}
}

func TestExpiredPurchaseAndProtectedRoutes(t *testing.T) {
	h := newHosting(t.TempDir())
	fixtureRegistry(t, h)
	if err := h.activate(t.Context(), approvalTestKey); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.validUntil = time.Now().Add(-time.Second)
	h.mu.Unlock()
	for _, action := range []string{"start", "settings", "command", "backup", "pack", "player", "folder", "import-world", "import-pack", "files"} {
		method := http.MethodPost
		if action == "files" {
			method = http.MethodGet
		}
		w := httptest.NewRecorder()
		hostingHTTP(h, w, httptest.NewRequest(method, "/api/hosting/"+action, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s returned %d", action, w.Code)
		}
	}
	h.expireLicense()
	if h.status().Licensed {
		t.Fatal("expired authorization retained")
	}
}

func TestProofAndDeviceBinding(t *testing.T) {
	record := grantMarker + " " + approvalTestNonce
	for _, ending := range []string{"", "\n", "\r\n"} {
		if !grantRecord.MatchString(record + ending) {
			t.Fatal("single-line approval rejected")
		}
	}
	if grantRecord.MatchString(record + "\n" + record) {
		t.Fatal("multiple records accepted")
	}
	subject := grantDigest(approvalTestPC, approvalTestKey)
	expected := approvalDigest(subject, approvalTestNonce)
	proof, err := os.ReadFile("testdata/hosting-attestation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = checkAttestation(proof, expected); err != nil {
		t.Fatal(err)
	}
	if checkAttestation(proof, approvalDigest(subject, "another purchase")) == nil {
		t.Fatal("proof accepted for another approval")
	}
	var modified map[string]any
	if err := json.Unmarshal(proof, &modified); err != nil {
		t.Fatal(err)
	}
	envelope := modified["dsseEnvelope"].(map[string]any)
	envelope["signatures"].([]any)[0].(map[string]any)["sig"] = "AA=="
	b, _ := json.Marshal(modified)
	if checkAttestation(b, expected) == nil || checkAttestation([]byte("MCBE2FRIENDS-PREMIUM-1"), expected) == nil {
		t.Fatal("invalid proof accepted")
	}
	salt := "PC-AAAAAAAAAAAAAAAA"
	one, err := boundPCCode(salt, "linux", strings.Repeat("1", 32))
	if err != nil {
		t.Fatal(err)
	}
	two, err := boundPCCode(salt, "linux", strings.Repeat("2", 32))
	if err != nil {
		t.Fatal(err)
	}
	again, err := boundPCCode(salt, "linux", strings.Repeat("1", 32))
	if err != nil || !devicePattern.MatchString(one) || one == two || one != again {
		t.Fatal("installation binding failed")
	}
	if _, err := boundPCCode(salt, "linux", strings.Repeat("0", 32)); err == nil {
		t.Fatal("empty OS identity accepted")
	}
}
