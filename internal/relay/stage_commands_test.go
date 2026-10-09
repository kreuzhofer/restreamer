package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestExpiredCommandOutcomeCannotBeReplayed(t *testing.T) {
	s := dashboardServer(t)
	state := readStage(t, s)
	cmd := StageCommand{ID: "old-stop", ServerID: state.ServerID, Context: state.Context, Action: "stop_now", Confirmed: true}
	body, _ := json.Marshal(cmd)
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for i := 0; i < 300; i++ {
		if w := stageRequest(t, s, "cancel_pending", map[string]any{"id": fmt.Sprintf("history-%d", i), "confirmed": false}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := dashboardRequest(s, "GET", "/api/stage/commands/old-stop", ""); w.Code != 404 {
		t.Fatal("Old command history was not expired", w.Code, w.Body.String())
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 409 {
		t.Fatal("Expired command could be replayed", w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "OFF" || state.Pending != nil {
		t.Fatal("Expired replay changed state", state)
	}
}

func TestPendingGoLiveSurvivesInvalidAndNoopActionsUntilExplicitCancellation(t *testing.T) {
	s, revision := preparedRevisionServer(t)
	if w := dashboardRequest(s, "PUT", "/api/stage-media", `{"prestream":"`+revision+`","shortcuts":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "prestream", map[string]any{"mode": "preview_only"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := readStage(t, s)
	cmd := StageCommand{ID: "pending-once", ServerID: state.ServerID, Context: state.Context, Action: "go_live", Confirmed: true}
	body, _ := json.Marshal(cmd)
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := readStage(t, s).Pending.Deadline
	for _, invalid := range []struct {
		action string
		extra  map[string]any
		code   int
	}{
		{"play_clip", map[string]any{"revision": "missing"}, 409},
		{"return", nil, 409},
		{"prestream", map[string]any{"confirmed": false}, 200},
		{"brb", map[string]any{"confirmed": false}, 409},
		{"seek", map[string]any{"position": -1, "confirmed": false}, 409},
	} {
		if w := stageRequest(t, s, invalid.action, invalid.extra); w.Code != invalid.code {
			t.Fatal(invalid.action, w.Code, w.Body.String())
		}
		if state := readStage(t, s); state.Stage != "PRESTREAM" || state.Pending == nil || state.Pending.ID != cmd.ID || state.Pending.Deadline != deadline {
			t.Fatal("Invalid or no-op action disturbed transition", invalid.action, state)
		}
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 202 {
		t.Fatal("Retry lost pending outcome", w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Pending.Deadline != deadline {
		t.Fatal("Duplicate restarted pending deadline", state)
	}
	if w := stageRequest(t, s, "cancel_pending", map[string]any{"confirmed": false}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state = readStage(t, s)
	if state.Stage != "PRESTREAM" || state.Pending != nil {
		t.Fatal("Cancel changed source", state)
	}
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 200 || !strings.Contains(w.Body.String(), `"cancelled"`) {
		t.Fatal("Duplicate cancelled command restarted", w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "go_live", map[string]any{"id": "superseded"}); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := stageRequest(t, s, "brb", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if state := readStage(t, s); state.Stage != "BRB" || state.Pending != nil {
		t.Fatal("Validated BRB did not supersede", state)
	}
	if w := dashboardRequest(s, "GET", "/api/stage/commands/superseded", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"cancelled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	cmd.ID, cmd.Context = "restart-replay", readStage(t, s).Context
	cmd.ServerID = "previous-process"
	body, _ = json.Marshal(cmd)
	if w := dashboardRequest(s, "POST", "/api/stage/commands", string(body)); w.Code != 409 {
		t.Fatal("Old process identity accepted", w.Code, w.Body.String())
	}
	s.broadcast.tick(time.Now())
	if readStage(t, s).Stage != "BRB" {
		t.Fatal("Rejected old command changed state")
	}
}
