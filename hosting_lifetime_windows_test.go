package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBedrockProcessLifetime(t *testing.T) {
	const childFlag = "MCBE2FRIENDS_JOB_TEST_CHILD"
	if os.Getenv(childFlag) == "1" {
		time.Sleep(time.Minute)
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBedrockProcessLifetime$")
	cmd.Env = append(os.Environ(), childFlag+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	release, err := bindChildLifetime(cmd.Process)
	if err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	release()
	_ = cmd.Wait()
	if ctx.Err() != nil || cmd.ProcessState == nil {
		t.Fatal("server process survived release of the launcher's job")
	}
}
