//go:build desktop

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"mcbe2friends/desktopwindow"
)

// TestMain puts Cocoa's application loop on the main thread of the child.
// Only the test executable contains this driver or its reporting handler.
func TestMain(m *testing.M) {
	if root := os.Getenv("MCBE2FRIENDS_DESKTOP_PROBE_DIR"); root != "" {
		if err := checkNativeWindow(root); err != nil {
			os.WriteFile(filepath.Join(root, "error"), []byte(err.Error()), 0600)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

type probeAssets struct {
	fs.FS
	extra fstest.MapFS
}

func (p probeAssets) Open(name string) (fs.File, error) {
	if file, ok := p.extra[name]; ok && file != nil {
		return p.extra.Open(name)
	}
	return p.FS.Open(name)
}

func checkNativeWindow(root string) error {
	l, err := newLauncher(root)
	if err != nil {
		return err
	}
	server := httptest.NewUnstartedServer(nil)
	instance := Instance{Port: server.Listener.Addr().(*net.TCPAddr).Port, Token: rand.Text()}
	actual := handler(l, instance, func() {})
	const bodySize = 1024*1024 + 37
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/test-file" {
			actual.ServeHTTP(w, r)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, bodySize+1))
		if err != nil || len(data) != bodySize || data[0] != 0 || data[bodySize-1] != 231 || r.Header.Get("Authorization") != "Bearer "+instance.Token {
			http.Error(w, "File body or authentication failed.", 400)
			return
		}
		writeJSON(w, 200, map[string]int{"bytes": len(data)})
	})
	server.Start()
	defer server.Close()
	config, closeRelay, err := launcherWindow(root, instance)
	if err != nil {
		return err
	}
	defer closeRelay()
	index, err := fs.ReadFile(config.Assets, "index.html")
	if err != nil {
		return err
	}
	index = bytes.Replace(index, []byte("</body>"), []byte("<script src=\"/desktop-probe.js\" defer></script></body>"), 1)
	config.Assets = probeAssets{config.Assets, fstest.MapFS{"index.html": &fstest.MapFile{Data: index}, "desktop-probe.js": &fstest.MapFile{Data: []byte(nativeProbe)}}}
	backend := config.Handler
	config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/probe-result" && r.Method == "GET" {
			data := []byte(r.URL.Query().Get("result"))
			os.WriteFile(filepath.Join(root, "report"), data, 0600)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("saved"))
			return
		}
		backend.ServeHTTP(w, r)
	})
	onClose := config.OnClose
	config.OnClose = func() { onClose(); os.WriteFile(filepath.Join(root, "closed"), []byte("closed"), 0600) }
	return desktopwindow.Show(config)
}

const nativeProbe = `
(async () => {
  const get = id => document.getElementById(id);
  const wait = async test => {for(let i=0;i<120;i++){if(test())return;await new Promise(r=>setTimeout(r,80));}throw Error("Controls did not become ready");};
  let report;
  try {
    await wait(()=>document.documentElement.classList.contains("desktop-app") && !get("connection-notice").textContent.trim() && !document.querySelector(".quit").hidden);
    get("hosting-nav").click();
    await wait(()=>!get("host-locked").hidden && !get("hosting-page").hidden);
    const env=await window.runtime.Environment();
    if (!env.platform) throw Error("Desktop platform unavailable");
    const fileData=new Uint8Array(1048613);fileData[fileData.length-1]=231;
    const reply=await fetch(apiURL("test-file"),{method:"POST",headers:{Authorization:"Bearer "+token,"Content-Type":"application/octet-stream"},body:new File([fileData],"test-pack.zip")});
    if (!reply.ok || (await reply.json()).bytes!==fileData.length) throw Error("Native file upload failed "+reply.status);
    const signedOut=await fetch(apiURL("state"));if(signedOut.status!==403) throw Error("Unexpected unauthenticated response "+signedOut.status);
    document.querySelector('[data-action="guide"]').click();
    if(!get("guide-dialog").open)throw Error("Guide did not open");
    get("guide-dialog").close();
    report={ok:true,platform:env.platform};
  } catch(e){report={error:String(e)};}
  await fetch("/probe-result?result="+encodeURIComponent(JSON.stringify(report)));
  document.querySelector(".quit").click();
  setTimeout(()=>window.runtime.Quit(),700);
})();
`

func TestNativeDesktopWindow(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_DESKTOP") != "1" {
		t.Skip("requires a graphical desktop")
	}
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "MCBE2FRIENDS_DESKTOP_PROBE_DIR="+dir)
	out, err := cmd.CombinedOutput()
	data, _ := os.ReadFile(filepath.Join(dir, "report"))
	problem, _ := os.ReadFile(filepath.Join(dir, "error"))
	var result struct {
		OK              bool
		Platform, Error string
	}
	if err != nil || json.Unmarshal(data, &result) != nil || !result.OK || result.Platform == "" {
		t.Fatalf("Embedded window: %v, %s %s %s", err, strings.TrimSpace(string(data)), problem, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "closed")); err != nil {
		t.Fatal("The native close callback did not run.")
	}
	t.Log("Opened the native window, managed Premium, streamed a file, opened the guide and closed on", result.Platform)
}
