package relay

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceNavigationPreservesBroadcastAndAuthentication(t *testing.T) {
	s := dashboardServer(t)
	before := dashboardRequest(s, "GET", "/api/stage", "").Body.String()
	for _, path := range []string{"/live", "/studio", "/settings", "/generator?id=bookmarked-design"} {
		unauthorized := httptest.NewRecorder()
		s.Handler().ServeHTTP(unauthorized, httptest.NewRequest("GET", path, nil))
		if unauthorized.Code != 401 {
			t.Fatalf("%s not protected: %d", path, unauthorized.Code)
		}
		response := dashboardRequest(s, "GET", path, "")
		if response.Code != 200 {
			t.Fatalf("%s: %d", path, response.Code)
		}
		for _, visible := range []string{`aria-label="Workspace"`, `href="/live"`, `href="/studio"`, `href="/settings"`, `id="workspace-status"`} {
			if !strings.Contains(response.Body.String(), visible) {
				t.Errorf("%s missing workspace element %s", path, visible)
			}
		}
	}
	var initial, final map[string]any
	json.Unmarshal([]byte(before), &initial)
	json.Unmarshal(dashboardRequest(s, "GET", "/api/stage", "").Body.Bytes(), &final)
	if initial["context"] != final["context"] || initial["stage"] != final["stage"] {
		t.Fatal("navigation changed broadcast")
	}
}

func TestLiveConsoleOffersMonitoringAndReadinessBeforeOperation(t *testing.T) {
	s := dashboardServer(t)
	response := dashboardRequest(s, "GET", "/live", "")
	for _, content := range []string{`aria-label="Broadcast operation"`, `aria-label="Before going on air"`, `id="readiness-input"`, `id="readiness-media"`, `id="readiness-destinations"`, `aria-label="Broadcast preview"`, `aria-label="Live input preview"`, `id="stage-rehearsal"`} {
		if !strings.Contains(response.Body.String(), content) {
			t.Errorf("Live console missing %s", content)
		}
	}
}
