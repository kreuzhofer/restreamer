// Package config loads the application's configuration without logging secrets.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Target struct {
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	StreamKey string     `json:"stream_key"`
	Enabled   EnableFlag `json:"enabled,omitempty"`
}

// EnableFlag accepts a JSON boolean or a string containing an environment
// reference. Its zero value means enabled, preserving existing configurations.
type EnableFlag string

func (f *EnableFlag) UnmarshalJSON(data []byte) error {
	if string(data) == "true" || string(data) == "false" {
		*f = EnableFlag(data)
		return nil
	}
	var value string
	if string(data) == "null" || json.Unmarshal(data, &value) != nil {
		return errors.New("enabled must be a boolean or an environment reference")
	}
	*f = EnableFlag(value)
	return nil
}

// IsEnabled reports whether the target can run: both the switch and key matter.
func (t Target) IsEnabled() bool {
	return t.Enabled != "false" && strings.TrimSpace(t.StreamKey) != ""
}

type BRBProfile struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	FPS        int `json:"fps"`
	SampleRate int `json:"sample_rate"`
}

func (b BRBProfile) Validate() error {
	if b.Width < 320 || b.Width > 3840 || b.Height < 180 || b.Height > 2160 || b.Width%2 != 0 || b.Height%2 != 0 || (b.FPS != 24 && b.FPS != 25 && b.FPS != 30 && b.FPS != 50 && b.FPS != 60) || (b.SampleRate != 44100 && b.SampleRate != 48000) {
		return errors.New("BRB needs an even resolution from 320x180 to 3840x2160, FPS 24/25/30/50/60, and sample_rate 44100/48000")
	}
	return nil
}

type BRBConfig struct {
	Enabled   EnableFlag `json:"enabled"`
	Directory string     `json:"directory"`
	BRBProfile
}

func (b *BRBConfig) IsEnabled() bool { return b != nil && b.Enabled == "true" }

type Config struct {
	BRB               *BRBConfig `json:"brb,omitempty"`
	Listen            string     `json:"listen"`
	HealthListen      string     `json:"health_listen"`
	Application       string     `json:"application"`
	StreamKey         string     `json:"stream_key"`
	QueueBytes        int        `json:"queue_bytes"`
	Targets           []Target   `json:"targets"`
	DashboardUsername string     `json:"dashboard_username"`
	DashboardPassword string     `json:"dashboard_password"`
	StateFile         string     `json:"state_file"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("cannot open configuration file")
	}
	defer f.Close()
	c := Config{Listen: ":1935", HealthListen: ":8080", Application: "live", QueueBytes: 16 << 20}
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, errors.New("invalid configuration JSON or unknown field")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("configuration must contain one JSON object")
	}
	// Expand individual decoded strings, so quotes or backslashes in a secret
	// cannot alter the structure of the configuration.
	values := []*string{&c.Listen, &c.HealthListen, &c.Application, &c.StreamKey}
	for _, value := range values {
		var missing bool
		*value, missing = expand(*value)
		if missing {
			return c, errors.New("configuration references an unset or empty environment variable")
		}
	}
	for i := range c.Targets {
		t := &c.Targets[i]
		t.Name = os.ExpandEnv(t.Name)
		flag, _ := expand(string(t.Enabled))
		t.Enabled = EnableFlag(strings.TrimSpace(flag))
		// An unset referenced key disables the output, including when the key
		// expression contains a literal prefix or suffix.
		var missing bool
		t.StreamKey, missing = expand(t.StreamKey)
		if missing {
			t.StreamKey = ""
		}
		t.URL, missing = expand(t.URL)
		if t.IsEnabled() && missing {
			return c, fmt.Errorf("target %d URL references an unset or empty environment variable", i+1)
		}
	}
	for _, value := range []*string{&c.DashboardUsername, &c.DashboardPassword} {
		var missing bool
		*value, missing = expand(*value)
		if missing {
			*value = ""
		}
	}
	c.StateFile = os.ExpandEnv(c.StateFile)
	if c.BRB != nil {
		c.BRB.Enabled = EnableFlag(os.ExpandEnv(string(c.BRB.Enabled)))
		c.BRB.Directory = os.ExpandEnv(c.BRB.Directory)
	}
	return c, c.Validate()
}

func expand(value string) (string, bool) {
	missing := false
	value = os.Expand(value, func(key string) string {
		v, ok := os.LookupEnv(key)
		if !ok || v == "" {
			missing = true
		}
		return v
	})
	return value, missing
}

func (c Config) Validate() error {
	if c.BRB != nil {
		b := c.BRB
		if b.Enabled != "" && b.Enabled != "true" && b.Enabled != "false" {
			return errors.New("brb.enabled must be true or false")
		}
		if b.IsEnabled() {
			if b.Directory == "" {
				return errors.New("BRB needs a persistent directory")
			}
			if err := b.BRBProfile.Validate(); err != nil {
				return err
			}
		}
	}
	if (c.DashboardUsername == "") != (c.DashboardPassword == "") {
		return errors.New("set both dashboard_username and dashboard_password, or leave both empty to disable the dashboard")
	}
	if strings.ContainsAny(c.DashboardUsername, ":\r\n") || strings.ContainsAny(c.DashboardPassword, "\r\n") {
		return errors.New("dashboard credentials contain unsupported characters")
	}
	for _, address := range []string{c.Listen, c.HealthListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("listen addresses must use host:port")
		}
	}
	if !identifier.MatchString(c.Application) {
		return errors.New("application must contain only letters, digits, underscores or hyphens")
	}
	if len(c.StreamKey) < 16 || len(c.StreamKey) > 256 || !identifier.MatchString(c.StreamKey) {
		return errors.New("input stream_key must contain 16–256 letters, digits, underscores or hyphens")
	}
	if c.QueueBytes < 1<<20 || c.QueueBytes > 256<<20 {
		return errors.New("queue_bytes must be between 1 MiB and 256 MiB")
	}
	if len(c.Targets) == 0 || len(c.Targets) > 16 {
		return errors.New("configure between 1 and 16 targets")
	}
	names := make(map[string]bool)
	for i, t := range c.Targets {
		if !identifier.MatchString(t.Name) || names[t.Name] {
			return fmt.Errorf("target %d needs a unique name using letters, digits, underscores or hyphens", i+1)
		}
		names[t.Name] = true
		if t.Enabled != "" && t.Enabled != "true" && t.Enabled != "false" {
			return fmt.Errorf("target %d enabled must be true or false", i+1)
		}
		if !t.IsEnabled() {
			continue
		}
		if err := t.ReadyError(); err != nil {
			return fmt.Errorf("target %d: %s", i+1, err)
		}
	}
	return nil
}

// ReadyError is safe to display: it never includes URLs or credentials.
func (t Target) ReadyError() error {
	if strings.TrimSpace(t.StreamKey) == "" {
		return errors.New("missing stream key")
	}
	u, err := url.Parse(t.URL)
	if err != nil || u == nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.Trim(u.Path, "/") == "" {
		return errors.New("needs an rtmp(s) server URL with an application path, without userinfo or fragment")
	}
	if u.Port() == "0" {
		return errors.New("invalid port")
	}
	return nil
}
