package main

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (h *Hosting) importWorld(ctx context.Context, path, name string) error {
	if !h.installed() {
		return errors.New("Start the server once before importing a world.")
	}
	dest, err := h.worldPath(name)
	if err != nil {
		return err
	}
	if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
		return errors.New("That world folder already exists. Choose a new name.")
	}
	stage, err := os.MkdirTemp(h.root, ".world-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	z, err := zip.OpenReader(path)
	if err != nil {
		return errors.New("Choose a readable .mcworld or ZIP file.")
	}
	defer z.Close()
	limit := int64(2 << 30)
	if err = unpack(ctx, &z.Reader, stage, &limit, 50000); err != nil {
		return err
	}
	src := stage
	if _, e := os.Stat(filepath.Join(src, "level.dat")); e != nil {
		entries, e := os.ReadDir(stage)
		if e != nil {
			return e
		}
		for _, d := range entries {
			if d.IsDir() && d.Name() != "__MACOSX" {
				src = filepath.Join(stage, d.Name())
				break
			}
		}
	}
	level, e := os.Stat(filepath.Join(src, "level.dat"))
	db, de := os.Stat(filepath.Join(src, "db"))
	if e != nil || de != nil || !level.Mode().IsRegular() || !db.IsDir() {
		return errors.New("A Bedrock world must contain level.dat and a db folder.")
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	return os.Rename(src, dest)
}
func (h *Hosting) backupWorld(ctx context.Context, name string) error {
	dir, err := h.worldPath(name)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(dir, "level.dat")); err != nil {
		return errors.New("Start this world once before backing it up.")
	}
	out := filepath.Join(h.root, "backups")
	if err = os.MkdirAll(out, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(out, ".backup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	z := zip.NewWriter(f)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("World backups support regular files only.")
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil {
			return e
		}
		entry, e := z.Create(filepath.ToSlash(rel))
		if e != nil {
			return e
		}
		src, e := os.Open(path)
		if e != nil {
			return e
		}
		defer src.Close()
		_, e = io.Copy(entry, src)
		return e
	})
	if closeErr := z.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	unique := strings.TrimPrefix(filepath.Base(f.Name()), ".backup-")
	return os.Rename(f.Name(), filepath.Join(out, name+"-"+time.Now().UTC().Format("20060102-150405")+"-"+unique+".mcworld"))
}
