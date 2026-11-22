package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const bedrockVersion = "1.26.66.1"

var bedrockAssets = map[string]asset{
	"linux":   {"bedrock-server-" + bedrockVersion + ".zip", "e87bcd95bec5cc34b065ca956825c6abb7a0c356ae79408302845069cecaa6bf", 98095547},
	"windows": {"bedrock-server-" + bedrockVersion + ".zip", "29b3fa28cc509cc11034f42d6be5e979263f7984f06fb465b4c42e3102e1d6ff", 88757286},
}

func unpack(ctx context.Context, z *zip.Reader, dir string, budget *int64, maxFiles int) error {
	if len(z.File) > maxFiles {
		return errors.New("This archive contains too many files.")
	}
	seen := map[string]bool{}
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" {
			continue
		}
		if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("The archive contains an unsupported file path.")
		}
		native, err := filepath.Localize(name)
		if err != nil || !filepath.IsLocal(native) {
			return errors.New("The archive contains an invalid filename.")
		}
		target := filepath.Join(dir, native)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() || seen[strings.ToLower(name)] || f.UncompressedSize64 > uint64(*budget) {
			return errors.New("The archive has duplicate, oversized, or unsupported files.")
		}
		seen[strings.ToLower(name)] = true
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			dst.Close()
			return err
		}
		n, copyErr := io.Copy(dst, io.LimitReader(src, int64(f.UncompressedSize64)+1))
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if uint64(n) != f.UncompressedSize64 {
			return errors.New("The archive ended unexpectedly.")
		}
		*budget -= n
	}
	return nil
}
func bedrockExe() string {
	if runtime.GOOS == "windows" {
		return "bedrock_server.exe"
	}
	return "bedrock_server"
}
func (h *Hosting) installed() bool {
	s, e := os.Stat(filepath.Join(h.server, bedrockExe()))
	return e == nil && s.Mode().IsRegular()
}
func (h *Hosting) install(ctx context.Context) error {
	if h.installed() {
		return nil
	}
	if _, e := os.Stat(h.server); e == nil {
		return errors.New("The server folder already exists without its Bedrock executable. Restore the official executable there or move the folder aside first.")
	}
	a, ok := bedrockAssets[runtime.GOOS]
	if !ok {
		return errors.New("This platform has no native Bedrock server package.")
	}
	archive := filepath.Join(h.root, a.name)
	if !verified(archive, a.digest) {
		system := "linux"
		if runtime.GOOS == "windows" {
			system = "win"
		}
		url := "https://www.minecraft.net/bedrockdedicatedserver/bin-" + system + "/" + a.name
		err := download(ctx, url, archive, a, func(p int) { h.progress("installing", "Downloading the official Bedrock server", p) })
		if err != nil {
			return err
		}
	}
	h.progress("installing", "Verifying and unpacking Bedrock", -1)
	stage, err := os.MkdirTemp(h.root, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	limit := int64(1 << 30)
	err = unpack(ctx, &z.Reader, stage, &limit, 20000)
	closeErr := z.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Chmod(filepath.Join(stage, bedrockExe()), 0700); err != nil {
		return err
	}
	c, err := h.config()
	if err != nil {
		return err
	}
	if err = c.apply(filepath.Join(stage, "server.properties")); err != nil {
		return err
	}
	// Keep a list of shipped packages so the library only shows player add-ons.
	var builtin []string
	for _, kind := range packDirs {
		entries, err := os.ReadDir(filepath.Join(stage, kind))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				builtin = append(builtin, kind+"/"+e.Name())
			}
		}
	}
	if err = atomicWrite(filepath.Join(stage, ".mcbe2friends-stock"), []byte(strings.Join(builtin, "\n"))); err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(stage, ".mcbe2friends-version"), []byte(bedrockVersion)); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(stage, h.server); err != nil {
		return fmt.Errorf("Could not install the server: %w", err)
	}
	// The verified install is now durable. Saved archives are unnecessary for restarts.
	_ = os.Remove(archive)
	return nil
}
