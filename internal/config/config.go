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
	Name      string `json:"name"`
	URL       string `json:"url"`
	StreamKey string `json:"stream_key"`
}

type Config struct {
	Listen       string   `json:"listen"`
	HealthListen string   `json:"health_listen"`
	Application  string   `json:"application"`
	StreamKey    string   `json:"stream_key"`
	QueueBytes   int      `json:"queue_bytes"`
	Targets      []Target `json:"targets"`
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
	for i := range c.Targets {
		values = append(values, &c.Targets[i].Name, &c.Targets[i].URL, &c.Targets[i].StreamKey)
	}
	for _, value := range values {
		missing := false
		*value = os.Expand(*value, func(key string) string {
			v, ok := os.LookupEnv(key)
			if !ok || v == "" {
				missing = true
			}
			return v
		})
		if missing {
			return c, errors.New("configuration references an unset or empty environment variable")
		}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
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
		u, err := url.Parse(t.URL)
		if err != nil || u == nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.Trim(u.Path, "/") == "" {
			return fmt.Errorf("target %d needs an rtmp(s) server URL with an application path, without userinfo or fragment", i+1)
		}
		if u.Port() == "0" {
			return fmt.Errorf("target %d has an invalid port", i+1)
		}
		if strings.TrimSpace(t.StreamKey) == "" {
			return fmt.Errorf("target %d needs a stream_key", i+1)
		}
	}
	return nil
}
