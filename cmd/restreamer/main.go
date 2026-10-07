package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kreuzhofer/restreamer/internal/config"
	"github.com/kreuzhofer/restreamer/internal/relay"
)

func main() { os.Exit(run()) }

func run() int {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		address := fs.String("url", "http://127.0.0.1:8080/healthz", "health endpoint")
		if fs.Parse(os.Args[2:]) != nil {
			return 2
		}
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(*address)
		if err != nil {
			return 1
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 1
		}
		return 0
	}
	fs := flag.NewFlagSet("restreamer", flag.ContinueOnError)
	path := fs.String("config", "config.json", "JSON configuration path")
	check := fs.Bool("check-config", false, "validate configuration and exit")
	if fs.Parse(os.Args[1:]) != nil || fs.NArg() != 0 {
		return 2
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(*path)
	if err != nil {
		log.Error("configuration error", "reason", err.Error())
		return 1
	}
	if *check {
		fmt.Println("configuration valid")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := relay.New(cfg, log).Run(ctx); err != nil {
		log.Error("server stopped", "reason", err.Error())
		return 1
	}
	log.Info("shutdown complete")
	return 0
}
