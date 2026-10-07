package config

import (
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
		"no targets":                     `{"stream_key":"input-key-1234567890"}`,
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
