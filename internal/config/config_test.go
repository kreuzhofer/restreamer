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
		"missing environment": strings.ReplaceAll(validJSON, "TARGET_KEY", "RESTREAMER_TEST_UNSET_987"),
		"unknown option":      strings.Replace(validJSON, "{", `{"typo":true,`, 1),
		"extra document":      validJSON + "{}",
		"weak input key":      strings.ReplaceAll(validJSON, "input-key-1234567890", "short"),
		"unsupported scheme":  strings.ReplaceAll(validJSON, "rtmps://", "https://"),
		"URL userinfo":        strings.ReplaceAll(validJSON, "example.com", "username:secret-not-for-logs@example.com"),
		"no targets":          `{"stream_key":"input-key-1234567890"}`,
		"null":                "null",
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
