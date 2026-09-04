// Package gitcmd — тонкие обёртки над git для рабочих папок агентов и задач.
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const cmdTimeout = 20 * time.Second

func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	full := append([]string{"--no-pager", "-c", "core.quotepath=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return out.String(), nil
}

func IsRepo(dir string) bool {
	_, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func HasCommits(dir string) bool {
	_, err := run(dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func HeadSHA(dir string) (string, error) {
	s, err := run(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(s), err
}

func Branch(dir string) (string, error) {
	s, err := run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(s), err
}

// FileChange — одна строка `git status --porcelain`: XY + путь.
type FileChange struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

const maxFiles = 500

func Status(dir string) ([]FileChange, error) {
	out, err := run(dir, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	files := make([]FileChange, 0, 16)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		p = strings.Trim(p, `"`)
		if p == "" {
			continue
		}
		files = append(files, FileChange{Status: line[:2], Path: p})
		if len(files) >= maxFiles {
			break
		}
	}
	return files, nil
}

// Diff — изменения рабочей копии относительно base (или HEAD, если base пуст).
func Diff(dir, base string) (string, error) {
	args := []string{"diff", "--no-color"}
	if base != "" {
		args = append(args, base)
	} else {
		args = append(args, "HEAD")
	}
	return run(dir, args...)
}

// Reset жёстко откатывает рабочую копию и HEAD к sha.
func Reset(dir, sha string) error {
	_, err := run(dir, "reset", "--hard", sha)
	return err
}
