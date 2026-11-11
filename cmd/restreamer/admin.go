package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
)

func adminStatus(args []string) int {
	fs := flag.NewFlagSet("admin status", flag.ContinueOnError)
	support := fs.Bool("check-support", false, "check whether this binary supports private update status")
	path := fs.String("config", "config.json", "running server configuration")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	if *support {
		return 0
	}
	cfg, err := config.Load(*path)
	if err != nil || cfg.UsersDatabasePath() == "" {
		fmt.Fprintln(os.Stderr, "Cannot resolve private administration socket from configuration")
		return 1
	}
	socket := cfg.UsersDatabasePath() + ".control/status.sock"
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://localhost/status")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Private status unavailable; check server health or deliberately stop it before updating")
		return 1
	}
	defer response.Body.Close()
	var status struct {
		Stage     string `json:"stage"`
		Pending   *bool  `json:"pending"`
		Publisher *bool  `json:"publisher_connected"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&status) != nil || status.Stage == "" || status.Pending == nil || status.Publisher == nil {
		fmt.Fprintln(os.Stderr, "Invalid private administration response")
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(status) != nil {
		return 1
	}
	return 0
}
