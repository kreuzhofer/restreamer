package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/relay"
)

// Exercise the real CLI entry point in a separate process, including stdin,
// exit codes and output. All credentials and storage belong to this test.
func TestAccountCLIProcess(t *testing.T) {
	if os.Getenv("RESTREAMER_TEST_CLI") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			os.Args = append([]string{args[0]}, args[i+1:]...)
			os.Exit(run())
		}
	}
	os.Exit(2)
}
func cli(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestAccountCLIProcess$", "--"}, args...)...)
	command.Env = append(os.Environ(), "RESTREAMER_TEST_CLI=1")
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	return string(output), err
}
func TestAccountResetCLIRevokesOldPasswordWithoutRestart(t *testing.T) {
	cfg := config.Config{DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(t.TempDir(), "targets.json")}
	server := relay.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(password string) int {
		r := httptest.NewRequest("GET", "/api/account", nil)
		r.SetBasicAuth("operator", password)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := request("original-password"); code != 200 {
		t.Fatalf("bootstrap: %d", code)
	}
	output, err := cli(t, "recovered-password-long\n", "account", "reset-password", "-database", cfg.UsersDatabasePath(), "-username", "operator", "-password-stdin")
	if err != nil {
		t.Fatalf("reset failed: %v %s", err, output)
	}
	if strings.Contains(output, "recovered-password-long") {
		t.Fatal("CLI disclosed password")
	}
	if request("original-password") != 401 || request("recovered-password-long") != 200 {
		t.Fatal("running server did not adopt reset")
	}
	output, err = cli(t, "recovered-password-long\n", "account", "reset-password", "-database", filepath.Join(t.TempDir(), "missing.sqlite"), "-username", "operator", "-password-stdin")
	if err == nil {
		t.Fatal("reset created a new account/database")
	}
}

func TestAdminStatusCLIReportsRunningServerWithoutDashboardPassword(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "rs-admin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	cfg := config.Config{Application: "live", StreamKey: "test-stream-key-123456", QueueBytes: 1 << 20, Listen: "127.0.0.1:0", HealthListen: "127.0.0.1:0", DashboardUsername: "operator", DashboardPassword: "original-password", StateFile: filepath.Join(directory, "targets.json")}
	server := relay.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("server: %v", err)
		}
	})
	path := filepath.Join(directory, "config.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := cli(t, "", "admin", "status", "-check-support")
	if err != nil {
		t.Fatalf("support probe: %v %s", err, output)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		output, err = cli(t, "", "admin", "status", "-config", path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status unavailable: %v %s", err, output)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var status struct {
		Stage     string `json:"stage"`
		Pending   bool   `json:"pending"`
		Publisher bool   `json:"publisher_connected"`
	}
	if json.Unmarshal([]byte(output), &status) != nil || status.Stage != "OFF" || status.Pending || status.Publisher {
		t.Fatalf("unexpected status: %s", output)
	}
	// The private socket must not be accessible to other OS users.
	info, err := os.Stat(cfg.UsersDatabasePath() + ".control/status.sock")
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("status socket is not private")
	}
}
