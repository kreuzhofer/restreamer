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
		for _, visible := range []string{`aria-label="Workspace"`, `href="/live"`, `href="/studio"`, `href="/settings"`, `id="workspace-status"`, `id="workspace-alert"`, `href="/live" class="workspace-alert-link"`} {
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

func TestBRBPreparationMovesToStudioAndKeepsLiveReview(t *testing.T) {
	s := dashboardServer(t)
	before := dashboardRequest(s, "GET", "/api/stage", "").Body.String()
	live := dashboardRequest(s, "GET", "/live", "")
	studio := dashboardRequest(s, "GET", "/studio?collection=brb", "")
	if live.Code != 200 || studio.Code != 200 {
		t.Fatal("BRB workspaces unavailable", live.Code, studio.Code)
	}
	if strings.Contains(live.Body.String(), `id="brb-form"`) || !strings.Contains(studio.Body.String(), `id="brb-form"`) {
		t.Fatal("BRB authoring must be available in Studio and absent from Live")
	}
	for _, page := range []string{live.Body.String(), studio.Body.String()} {
		for _, action := range []string{"Preview exact BRB", "Activate prepared BRB"} {
			if !strings.Contains(page, action) {
				t.Fatal("exact candidate operation missing", action)
			}
		}
	}
	if after := dashboardRequest(s, "GET", "/api/stage", "").Body.String(); after != before {
		t.Fatal("opening BRB preparation changed the broadcast")
	}
}

func TestPreparedMediaIsAvailableOnDemandWithoutLiveUploadControls(t *testing.T) {
	s := dashboardServer(t)
	page := dashboardRequest(s, "GET", "/live", "").Body.String()
	for _, control := range []string{`<dialog id="prepared-media-drawer"`, `id="prepared-media-open"`, `id="clip-playback-controls"`, `id="library-loading"`, `href="/studio?collection=media"`} {
		if !strings.Contains(page, control) {
			t.Errorf("missing prepared-media control %s", control)
		}
	}
	if strings.Contains(page, `id="library-upload"`) {
		t.Fatal("Live still exposes media upload instead of Studio")
	}
}

func TestSettingsProvidesAuthenticatedDestinationManagement(t *testing.T) {
	s := dashboardServer(t)
	page := dashboardRequest(s, "GET", "/settings", "")
	for _, content := range []string{`data-destination-settings`, `src="/destination-settings.js"`, `href="/destination-settings.css"`} {
		if !strings.Contains(page.Body.String(), content) {
			t.Errorf("Settings missing destination management: %s", content)
		}
	}
	for _, path := range []string{"/destination-settings.js", "/destination-settings.css"} {
		unauthorized := httptest.NewRecorder()
		s.Handler().ServeHTTP(unauthorized, httptest.NewRequest("GET", path, nil))
		if unauthorized.Code != 401 {
			t.Fatal("unprotected Settings asset", path, unauthorized.Code)
		}
	}
}
