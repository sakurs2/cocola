package operationlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const filename = ".operation.lock"

type metadata struct {
	PID       int    `json:"pid"`
	Command   string `json:"command"`
	StartedAt string `json:"started_at"`
}

type Lock struct {
	file *os.File
}

func Acquire(home, command string) (*Lock, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create Cocola installation directory for operation lock: %w", err)
	}
	path := filepath.Join(home, filename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Cocola operation lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("secure Cocola operation lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		owner := readMetadata(file)
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, busyError(owner)
		}
		return nil, fmt.Errorf("acquire Cocola operation lock: %w", err)
	}
	owner := metadata{
		PID:       os.Getpid(),
		Command:   command,
		StartedAt: time.Now().Format(time.RFC3339),
	}
	contents, err := json.Marshal(owner)
	if err == nil {
		err = file.Truncate(0)
	}
	if err == nil {
		_, err = file.Seek(0, 0)
	}
	if err == nil {
		_, err = file.Write(append(contents, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
		return nil, fmt.Errorf("record Cocola operation lock owner: %w", err)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(
		unix.Flock(int(file.Fd()), unix.LOCK_UN),
		file.Close(),
	)
}

func readMetadata(file *os.File) metadata {
	if _, err := file.Seek(0, 0); err != nil {
		return metadata{}
	}
	var owner metadata
	_ = json.NewDecoder(file).Decode(&owner)
	return owner
}

func busyError(owner metadata) error {
	details := make([]string, 0, 3)
	if owner.Command != "" {
		details = append(details, "command: "+owner.Command)
	}
	if owner.PID > 0 {
		details = append(details, fmt.Sprintf("PID: %d", owner.PID))
	}
	if owner.StartedAt != "" {
		details = append(details, "started: "+owner.StartedAt)
	}
	message := "another Cocola operation is already running"
	if len(details) > 0 {
		message += " (" + strings.Join(details, ", ") + ")"
	}
	return errors.New(message)
}
