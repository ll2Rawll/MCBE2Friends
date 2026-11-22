package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

const (
	buildNumber = "165"
	jarURL      = "https://github.com/MCXboxBroadcast/Broadcaster/releases/download/165/MCXboxBroadcastStandalone.jar"
	jarDigest   = "3bb50e70d640a476f67e14cc19c7ca4abf525348ef0e19eeb7feaafc1d48a5af"
	runtimeTag  = "21.0.13.1+2"
	runtimeBase = "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.13.1%2B2/"
)

type asset struct {
	name, digest string
	size         int64
}

// Digests are from Eclipse Adoptium's jdk-21.0.13.1+2 release manifest.
var runtimes = map[string]asset{
	"windows/amd64": {"OpenJDK21U-jre_x64_windows_hotspot_21.0.13.1_2.zip", "e15ee1e54a5a3b38027a09cedfa87bc3348d7dd3ce50866dd3d9671d7fffc6c1", 49108427},
	"darwin/arm64":  {"OpenJDK21U-jre_aarch64_mac_hotspot_21.0.13.1_2.tar.gz", "848feb1906c7de79de4f8391636042193fbd46eff86e26299506c4656f62c260", 47946379},
	"darwin/amd64":  {"OpenJDK21U-jre_x64_mac_hotspot_21.0.13.1_2.tar.gz", "10cee6ba4cd83fd4613a3f0bb3ef9dddd68c68d5f3ccfcc7e456707c9e1eafe0", 41907486},
	"linux/amd64":   {"OpenJDK21U-jre_x64_linux_hotspot_21.0.13.1_2.tar.gz", "18f44fab9065217f3ba61d0eef8a7b5f3c3713aa7293001b0d4ac10ede3850f0", 52190567},
	"linux/arm64":   {"OpenJDK21U-jre_aarch64_linux_hotspot_21.0.13.1_2.tar.gz", "52bc58323d9266538a6361a3cf8365fc1186dd9510d7f33f623681c4aabb7e72", 51283573},
}

type reportProgress func(message string, percent int)

func verified(path, digest string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return err == nil && hex.EncodeToString(h.Sum(nil)) == digest
}

// Download to a sibling temp file and publish it only after size and digest
// match. A cancelled or interrupted run cannot become a cached installation.
func download(ctx context.Context, url, dest string, a asset, report func(int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mcbe2Friends/1.1")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("The download could not connect. Check your internet connection and try again.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("The download server returned HTTP %d. Try again in a moment.", resp.StatusCode)
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	writer := io.MultiWriter(f, h)
	buf := make([]byte, 64*1024)
	var total int64
	prev := -1
	for {
		n, readErr := resp.Body.Read(buf)
		total += int64(n)
		if total > a.size {
			return errors.New("The download size does not match the release. Retry to fetch a fresh copy.")
		}
		if n > 0 {
			if _, err := writer.Write(buf[:n]); err != nil {
				return err
			}
			if percent := int(total * 100 / a.size); percent != prev {
				report(percent)
				prev = percent
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return errors.New("The download was interrupted. Check your connection and try again.")
		}
	}
	if total != a.size || hex.EncodeToString(h.Sum(nil)) != a.digest {
		return errors.New("The download did not pass verification. Retry to fetch a fresh copy.")
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dest)
}

var java21 = regexp.MustCompile(`(?m)^\s*java\.specification\.version\s*=\s*21\s*$`)

func usableJava(ctx context.Context, path string) bool {
	if path == "" {
		return false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return false
	}
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probe, path, "-XshowSettings:properties", "-version")
	configureChild(cmd)
	out, err := cmd.CombinedOutput()
	return err == nil && java21.Match(out)
}

func javaName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func ensureJava(ctx context.Context, root string, report reportProgress) (string, error) {
	report("Checking for Java 21", -1)
	relJava := filepath.Join("bin", javaName())
	managedRel := relJava
	if runtime.GOOS == "darwin" {
		managedRel = filepath.Join("Contents", "Home", relJava)
	}
	dest := filepath.Join(root, "java-"+runtimeTag+"-"+runtime.GOARCH)
	managedJava := filepath.Join(dest, managedRel)

	candidates := []string{managedJava}
	if home := os.Getenv("JAVA_HOME"); filepath.IsAbs(home) {
		candidates = append(candidates, filepath.Join(home, relJava))
	}
	if path, err := exec.LookPath(javaName()); err == nil && !(runtime.GOOS == "darwin" && path == "/usr/bin/java") {
		candidates = append(candidates, path)
	}
	for _, path := range candidates {
		if usableJava(ctx, path) {
			return path, nil
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	a, ok := runtimes[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", errors.New("Install Java 21 for this computer, then start the broadcast again.")
	}

	// tar is included in supported Windows 10/11, macOS and Linux systems.
	tar, err := exec.LookPath("tar")
	if err != nil {
		return "", errors.New("Install the system tar utility or Java 21, then try again.")
	}
	work, err := os.MkdirTemp(root, ".java-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	archive := filepath.Join(work, a.name)
	report("Downloading Java 21", 0)
	if err := download(ctx, runtimeBase+a.name, archive, a, func(p int) { report("Downloading Java 21", p) }); err != nil {
		return "", fmt.Errorf("Java setup: %w", err)
	}
	report("Installing Java 21", -1)
	unpacked := filepath.Join(work, "unpacked")
	if err := os.Mkdir(unpacked, 0700); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, tar, "-xf", a.name, "-C", "unpacked")
	cmd.Dir = work
	configureChild(cmd)
	if err := cmd.Run(); err != nil {
		return "", errors.New("Java could not be unpacked. Check free disk space and try again.")
	}

	entries, err := os.ReadDir(unpacked)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		from := filepath.Join(unpacked, entry.Name())
		if entry.IsDir() && usableJava(ctx, filepath.Join(from, managedRel)) {
			// Move a damaged previous installation aside only after the replacement works.
			if _, err := os.Stat(dest); err == nil {
				if err := os.Rename(dest, filepath.Join(work, "previous")); err != nil {
					return "", err
				}
			}
			if err := os.Rename(from, dest); err != nil {
				return "", err
			}
			return managedJava, nil
		}
	}
	return "", errors.New("The downloaded Java runtime could not start on this computer. Install Java 21 and try again.")
}

func prepare(ctx context.Context, root string, report reportProgress) (java, jar string, err error) {
	dir := filepath.Join(root, "components")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	java, err = ensureJava(ctx, dir, report)
	if err != nil {
		return "", "", err
	}
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	jar = filepath.Join(dir, "MCXboxBroadcastStandalone-"+buildNumber+".jar")
	report("Checking MCXboxBroadcast", -1)
	if !verified(jar, jarDigest) {
		a := asset{"MCXboxBroadcastStandalone.jar", jarDigest, 19600632}
		report("Downloading MCXboxBroadcast", 0)
		if err := download(ctx, jarURL, jar, a, func(p int) { report("Downloading MCXboxBroadcast", p) }); err != nil {
			return "", "", fmt.Errorf("Broadcaster setup: %w", err)
		}
	}
	return java, jar, nil
}
