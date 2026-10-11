// Package config reads the application configuration from environment variables.
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	SMTPPort       int
	SMTPDomain     string
	SMTPMaxBytes   int64
	SMTPTimeout    time.Duration
	SMTPMaxRcpts   int
	Whitelist      []string // normalized extensions without dot, e.g. "pdf"
	PaperlessURL   string
	PaperlessToken string
	Tags           []string
	Correspondent  string
	DocumentType   string
	HTTPTimeout    time.Duration
	UploadRetries  int
	MetricsPort    int
	LogLevel       slog.Level
	LogJSON        bool
}

// LoadDotEnv loads KEY=VALUE pairs from path into the process environment.
// Variables that are already set are never overridden. A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

// Load builds a Config from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		SMTPDomain:    env("SMTP_DOMAIN", "localhost"),
		PaperlessURL:  strings.TrimRight(os.Getenv("PAPERLESS_URL"), "/"),
		Correspondent: os.Getenv("PAPERLESS_CORRESPONDENT"),
		DocumentType:  os.Getenv("PAPERLESS_DOCUMENT_TYPE"),
	}

	var errs []string
	fail := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	var err error

	c.SMTPMaxBytes, err = envInt64("SMTP_MAX_MESSAGE_BYTES", 50<<20)
	fail(err)
	c.SMTPMaxRcpts, err = envInt("SMTP_MAX_RECIPIENTS", 50)
	fail(err)
	c.SMTPTimeout, err = envDuration("SMTP_TIMEOUT", 5*time.Minute)
	fail(err)
	c.HTTPTimeout, err = envDuration("PAPERLESS_HTTP_TIMEOUT", 60*time.Second)
	fail(err)
	c.UploadRetries, err = envInt("PAPERLESS_UPLOAD_RETRIES", 3)
	fail(err)
	c.SMTPPort, err = envPort("SMTP_PORT", 2525)
	fail(err)
	c.MetricsPort, err = envPort("METRICS_PORT", 9090)
	fail(err)

	for _, e := range strings.Split(env("ATTACHMENT_WHITELIST", "tiff,pdf,jpg"), ",") {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			c.Whitelist = append(c.Whitelist, e)
		}
	}
	if len(c.Whitelist) == 0 {
		errs = append(errs, "ATTACHMENT_WHITELIST must not be empty")
	}

	for _, t := range strings.Split(os.Getenv("PAPERLESS_TAGS"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			c.Tags = append(c.Tags, t)
		}
	}

	if err := c.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Sprintf("LOG_LEVEL: %v (use debug, info, warn, error)", err))
	}
	switch f := strings.ToLower(env("LOG_FORMAT", "text")); f {
	case "text":
	case "json":
		c.LogJSON = true
	default:
		errs = append(errs, fmt.Sprintf("LOG_FORMAT: %q (use text or json)", f))
	}

	c.PaperlessToken = strings.TrimSpace(os.Getenv("PAPERLESS_TOKEN"))
	if file := os.Getenv("PAPERLESS_TOKEN_FILE"); c.PaperlessToken == "" && file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, fmt.Sprintf("PAPERLESS_TOKEN_FILE: %v", err))
		}
		c.PaperlessToken = strings.TrimSpace(string(b))
	}
	if c.PaperlessToken == "" {
		errs = append(errs, "PAPERLESS_TOKEN (or PAPERLESS_TOKEN_FILE) is required")
	}

	if u, err := url.Parse(c.PaperlessURL); err != nil || c.PaperlessURL == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, "PAPERLESS_URL is required and must be an http(s) URL")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) (int64, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: %q is not a non-negative integer", key, v)
	}
	return n, nil
}

func envInt(key string, def int) (int, error) {
	n, err := envInt64(key, int64(def))
	return int(n), err
}

func envPort(key string, def int) (int, error) {
	n, err := envInt(key, def)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s: must be a port number between 1 and 65535", key)
	}
	return n, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive duration (e.g. 30s, 5m)", key, v)
	}
	return d, nil
}
