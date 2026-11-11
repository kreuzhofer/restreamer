package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type twitchIngest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Twitch's public discovery API is queried from the relay's network. Stream
// keys are never sent to this API, and address schemes/ports are never invented.
func fetchTwitchIngests(ctx context.Context, client *http.Client) ([]twitchIngest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ingest.twitch.tv/ingests", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Twitch ingest discovery is unavailable; retry or enter a server URL manually")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("Twitch ingest discovery returned an error; retry")
	}
	var data struct {
		Ingests []struct {
			Name   string `json:"name"`
			Secure string `json:"url_template_secure"`
		} `json:"ingests"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&data) != nil {
		return nil, errors.New("Twitch returned an invalid server list")
	}
	servers := []twitchIngest{}
	seen := map[string]bool{}
	for _, item := range data.Ingests {
		if !strings.HasSuffix(item.Secure, "/{stream_key}") {
			continue
		}
		address := strings.TrimSuffix(item.Secure, "/{stream_key}")
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "rtmps" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/app" || !(strings.HasSuffix(u.Hostname(), ".live-video.net") || strings.HasSuffix(u.Hostname(), ".twitch.tv")) || len(item.Name) > 180 || strings.TrimSpace(item.Name) == "" || seen[address] {
			continue
		}
		seen[address] = true
		servers = append(servers, twitchIngest{Name: item.Name, URL: address})
	}
	if len(servers) == 0 {
		return nil, errors.New("Twitch returned no usable secure ingest servers; retry or enter a server URL manually")
	}
	return servers, nil
}
func (s *Server) twitchIngestsHTTP(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected ingest redirect") }}
	servers, err := fetchTwitchIngests(r.Context(), client)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	generatorJSON(w, 200, servers)
}
