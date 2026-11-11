package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ingestTransport func(*http.Request) (*http.Response, error)

func (f ingestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTwitchIngestDiscoveryUsesExactSecureTemplate(t *testing.T) {
	client := &http.Client{Transport: ingestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://ingest.twitch.tv/ingests" || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected ingest request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ingests":[{"name":"Frankfurt","url_template":"rtmp://euc10.contribute.live-video.net/app/{stream_key}","url_template_secure":"rtmps://euc10.contribute.live-video.net/app/{stream_key}"}]}`))}, nil
	})}
	servers, err := fetchTwitchIngests(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].URL != "rtmps://euc10.contribute.live-video.net/app" {
		t.Fatalf("invented address: %+v", servers)
	}
}
func TestTwitchIngestDiscoveryRejectsUnsafeAndUnusableResponses(t *testing.T) {
	for _, body := range []string{`{"ingests":[]}`, `{"ingests":[{"name":"Bad","url_template_secure":"http://localhost/app/{stream_key}"}]}`, `{"ingests":[{"name":"Bad","url_template_secure":"rtmps://evil.test/app/{stream_key}"}]}`, `{"ingests":[{"name":"Bad","url_template_secure":"rtmps://key@euc10.contribute.live-video.net/app/{stream_key}"}]}`} {
		client := &http.Client{Transport: ingestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := fetchTwitchIngests(context.Background(), client); err == nil {
			t.Fatal("accepted unsafe response", body)
		}
	}
}

func TestTwitchIngestDiscoveryRequiresDashboardAuthentication(t *testing.T) {
	s := dashboardServer(t)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/destination-presets/twitch", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("discovery is unprotected: %d", response.Code)
	}
}

func TestTwitchIngestDiscoveryReportsUpstreamFailures(t *testing.T) {
	for _, status := range []int{429, 500, 503} {
		client := &http.Client{Transport: ingestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private upstream detail"))}, nil
		})}
		if _, err := fetchTwitchIngests(context.Background(), client); err == nil || strings.Contains(err.Error(), "private upstream detail") {
			t.Fatal("missing or unsafe upstream error", err)
		}
	}
	client := &http.Client{Transport: ingestTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("private peer details") })}
	if _, err := fetchTwitchIngests(context.Background(), client); err == nil || strings.Contains(err.Error(), "private peer") {
		t.Fatal("missing or unsafe transport error", err)
	}
}
