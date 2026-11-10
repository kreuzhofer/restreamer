package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadText(t *testing.T, body string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

const validJSON = `{"stream_key":"input-key-1234567890","targets":[{"name":"test","url":"rtmps://example.com/live","stream_key":"${TARGET_KEY}"}]}`

func TestLoadExpandsSecretsAfterJSONDecode(t *testing.T) {
	secret := "quotes\"slashes\\dollars$anything"
	t.Setenv("TARGET_KEY", secret)
	c, err := loadText(t, validJSON)
	if err != nil {
		t.Fatal(err)
	}
	if c.Targets[0].StreamKey != secret || c.Listen != ":1935" || c.QueueBytes != 16<<20 {
		t.Fatalf("unexpected config defaults/expansion")
	}
}

func TestInvalidConfig(t *testing.T) {
	t.Setenv("TARGET_KEY", "secret-not-for-logs")
	for name, body := range map[string]string{
		"missing input key environment":  strings.ReplaceAll(validJSON, "input-key-1234567890", "${RESTREAMER_TEST_UNSET_987}"),
		"missing active URL environment": strings.ReplaceAll(validJSON, "rtmps://example.com/live", "${RESTREAMER_TEST_UNSET_987}"),
		"unknown option":                 strings.Replace(validJSON, "{", `{"typo":true,`, 1),
		"extra document":                 validJSON + "{}",
		"weak input key":                 strings.ReplaceAll(validJSON, "input-key-1234567890", "short"),
		"unsupported scheme":             strings.ReplaceAll(validJSON, "rtmps://", "https://"),
		"URL userinfo":                   strings.ReplaceAll(validJSON, "example.com", "username:secret-not-for-logs@example.com"),
		"null":                           "null",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadText(t, body)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if strings.Contains(err.Error(), "secret-not-for-logs") {
				t.Fatal("error leaked secret")
			}
		})
	}
}

func TestTargetEnablement(t *testing.T) {
	t.Setenv("RESTREAMER_TEST_EMPTY_KEY", "")
	t.Setenv("RESTREAMER_TEST_FLAG_FALSE", "false")
	t.Setenv("RESTREAMER_TEST_FLAG_TRUE", "true")
	t.Setenv("RESTREAMER_TEST_FLAG_INVALID", "yes")
	for _, tc := range []struct {
		name    string
		target  string
		want    bool
		invalid bool
	}{
		{"omitted defaults true", `"stream_key":"key"`, true, false},
		{"explicit true", `"enabled":true,"stream_key":"key"`, true, false},
		{"explicit false overrides key", `"enabled":false,"stream_key":"key"`, false, false},
		{"missing key", `"enabled":true`, false, false},
		{"empty key", `"stream_key":""`, false, false},
		{"whitespace key", `"stream_key":"  "`, false, false},
		{"empty environment key", `"stream_key":"${RESTREAMER_TEST_EMPTY_KEY}"`, false, false},
		{"unset environment key", `"stream_key":"${RESTREAMER_TEST_UNSET_987}"`, false, false},
		{"partial missing key", `"stream_key":"prefix-${RESTREAMER_TEST_UNSET_987}"`, false, false},
		{"true environment flag", `"enabled":"${RESTREAMER_TEST_FLAG_TRUE}","stream_key":"key"`, true, false},
		{"false environment flag", `"enabled":"${RESTREAMER_TEST_FLAG_FALSE}","stream_key":"key"`, false, false},
		{"unset flag defaults true", `"enabled":"${RESTREAMER_TEST_UNSET_987}","stream_key":"key"`, true, false},
		{"invalid flag", `"enabled":"${RESTREAMER_TEST_FLAG_INVALID}","stream_key":"key"`, false, true},
		{"numeric flag", `"enabled":0,"stream_key":"key"`, false, true},
		{"null flag", `"enabled":null,"stream_key":"key"`, false, true},
		{"disabled invalid flag is still rejected", `"enabled":"yes"`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadText(t, `{"stream_key":"input-key-1234567890","targets":[{"name":"test","url":"rtmp://localhost/app",`+tc.target+`}]}`)
			if tc.invalid {
				if err == nil {
					t.Fatal("expected invalid flag rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Targets[0].IsEnabled() != tc.want {
				t.Fatalf("enabled = %v, want %v", c.Targets[0].IsEnabled(), tc.want)
			}
		})
	}
}

func TestDisabledTargetsNeedNoURL(t *testing.T) {
	for _, target := range []string{
		`{"name":"test"}`,
		`{"name":"test","stream_key":"key","enabled":false}`,
		`{"name":"test","url":"${RESTREAMER_TEST_UNSET_987}"}`,
	} {
		c, err := loadText(t, `{"stream_key":"input-key-1234567890","targets":[`+target+`]}`)
		if err != nil {
			t.Fatal(err)
		}
		if c.Targets[0].IsEnabled() {
			t.Fatal("unexpected active target")
		}
	}
}

func TestDashboardCredentials(t *testing.T) {
	t.Setenv("DASH_TEST_USER", "admin")
	t.Setenv("DASH_TEST_PASSWORD", `password$with"quotes`)
	for _, tc := range []struct {
		fields         string
		valid, enabled bool
	}{
		{``, true, false},
		{`,"dashboard_username":"${DASH_TEST_USER}","dashboard_password":"${DASH_TEST_PASSWORD}"`, true, true},
		{`,"dashboard_username":"admin"`, false, false},
		{`,"dashboard_password":"secret"`, false, false},
		{`,"dashboard_username":"admin:bad","dashboard_password":"secret"`, false, false},
		{`,"dashboard_username":"${UNSET_DASH_987}","dashboard_password":"prefix-${UNSET_DASH_987}"`, true, false},
	} {
		cfg, err := loadText(t, `{"stream_key":"input-key-1234567890","targets":[{"name":"one"}]`+tc.fields+`}`)
		if (err == nil) != tc.valid {
			t.Fatalf("valid %v, error %v", tc.valid, err)
		}
		if err == nil && (cfg.DashboardPassword != "") != tc.enabled {
			t.Fatal("wrong dashboard default")
		}
		if tc.enabled && cfg.DashboardPassword != `password$with"quotes` {
			t.Fatal("password changed during expansion")
		}
	}
}

func TestMediaConfigurationAlwaysAvailable(t *testing.T) {
	t.Setenv("BRB_TEST_ENABLED", "false")
	for _, fields := range []string{"", `,"brb":null`, `,"brb":{}`, `,"brb":{"enabled":false}`, `,"brb":{"enabled":"${BRB_TEST_ENABLED}"}`, `,"brb":{"enabled":"${REMOVED_BRB_FLAG_987}"}`} {
		c, err := loadText(t, strings.TrimSuffix(validJSON, "}")+fields+"}")
		if err != nil || c.BRB == nil {
			t.Fatalf("media defaults unavailable for %s: %v", fields, err)
		}
		if c.BRB.Directory == "" || c.BRB.Width != 1920 || c.BRB.Height != 1080 || c.BRB.FPS != 30 || c.BRB.SampleRate != 48000 {
			t.Fatalf("unexpected media defaults: %+v", c.BRB)
		}
		encoded, err := json.Marshal(c.BRB)
		if err != nil || strings.Contains(string(encoded), `"enabled"`) {
			t.Fatal("retired media switch remains in configuration", string(encoded), err)
		}
	}
}

func TestBRBConfigurationValidation(t *testing.T) {
	base := strings.TrimSuffix(validJSON, "}") + `,"brb":{"enabled":false,"directory":"${BRB_TEST_DIR}","width":1920,"height":1080,"fps":25,"sample_rate":48000}}`
	t.Setenv("BRB_TEST_DIR", t.TempDir())
	c, err := loadText(t, base)
	if err != nil || c.BRB == nil || c.BRB.FPS != 25 {
		t.Fatal("valid media config rejected", err)
	}
	for _, pair := range [][2]string{{`"fps":25`, `"fps":27`}, {`"width":1920`, `"width":1921`}, {`"sample_rate":48000`, `"sample_rate":96000`}, {`"directory":"${BRB_TEST_DIR}"`, `"directory":""`}, {`"fps":25`, `"fps":0`}, {`"fps":25`, `"typo":25`}} {
		if _, err = loadText(t, strings.Replace(base, pair[0], pair[1], 1)); err == nil {
			t.Fatal("invalid media config accepted despite retired switch", pair)
		}
	}
}

func TestEmptyDestinationSetup(t *testing.T) {
	if _, err := loadText(t, `{"stream_key":"input-key-1234567890","targets":[]}`); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDestinationsIgnoreRemovedLegacyEnvironment(t *testing.T) {
	directory := t.TempDir()
	credentials := filepath.Join(directory, "destinations.json")
	if err := os.WriteFile(credentials, []byte(`{"version":1,"targets":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := `{"stream_key":"input-key-1234567890","destinations_file":"` + credentials + `","targets":[{"name":"legacy","url":"${RESTREAMER_MISSING_LEGACY_URL}","stream_key":"old-key"}]}`
	cfg, err := loadText(t, body)
	if err != nil || len(cfg.Targets) != 0 {
		t.Fatal("managed file did not supersede legacy definitions", err)
	}
}

func TestManagedIngestionConfigurationNeedsNoEnvironmentKey(t *testing.T) {
	for _, legacy := range []string{"", `,"stream_key":"${RESTREAMER_TEST_UNSET_987}"`, `,"stream_key":"legacy-ingest-key-123456"`} {
		c, err := loadText(t, `{"ingestion_key_file":"/private/ingestion-key.json","targets":[{"name":"disabled"}]`+legacy+`}`)
		if err != nil || c.IngestionKeyFile != "/private/ingestion-key.json" {
			t.Fatal("managed configuration requires legacy key", err)
		}
	}
}

func TestTargetNamesAllowSpacesWithoutRelaxingIngestionIdentifiers(t *testing.T) {
	t.Setenv("TARGET_KEY", "test-key")
	if _, err := loadText(t, strings.Replace(validJSON, `"name":"test"`, `"name":"Twitch Main"`, 1)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		strings.Replace(validJSON, "input-key-1234567890", "input key 1234567890", 1),
		strings.Replace(validJSON, "{", `{"application":"live app",`, 1),
	} {
		if _, err := loadText(t, body); err == nil {
			t.Fatal("ingestion identifier accepted spaces")
		}
	}
}

func TestPublicIngestionAddress(t *testing.T) {
	t.Setenv("TARGET_KEY", "test-target-key")
	t.Setenv("INGESTION_SERVER_URL", "rtmp://studio.example.com:1935/live")
	c, err := loadText(t, strings.Replace(validJSON, "{", `{"ingestion_server_url":"${INGESTION_SERVER_URL}",`, 1))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(c)
	if err != nil || !strings.Contains(string(encoded), `"ingestion_server_url":"rtmp://studio.example.com:1935/live"`) {
		t.Fatal("public URL not expanded and retained")
	}
	for _, address := range []string{"https://studio.example.com:1935/live", "rtmp://studio.example.com/live", "rtmp://studio.example.com:0/live", "rtmp://studio.example.com:65536/live", "rtmp://user:secret@studio.example.com:1935/live", "rtmp://studio.example.com:1935/live/key", "rtmp://studio.example.com:1935/live?key=secret", "rtmp://studio.example.com:1935/live#secret"} {
		t.Setenv("INGESTION_SERVER_URL", address)
		_, err := loadText(t, strings.Replace(validJSON, "{", `{"ingestion_server_url":"${INGESTION_SERVER_URL}",`, 1))
		if err == nil {
			t.Error("accepted invalid public address")
		} else if strings.Contains(err.Error(), "secret") {
			t.Error("validation leaked URL contents")
		}
	}
}
