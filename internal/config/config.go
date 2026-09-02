// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config is everything the service needs to start.
type Config struct {
	ListenAddr string
	APIKey     string

	IMAPAddr      string
	SMTPAddr      string
	Username      string
	Password      string
	TLSSkipVerify bool

	FromAddress string
	FromName    string

	AllowSend   bool
	AllowDelete bool
	AllowPurge  bool

	MaxBodyChars       int
	DefaultSearchLimit int
	MaxSearchLimit     int
}

// FromEnv reads configuration, applying Proton Bridge defaults.
func FromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:         envString("LISTEN_ADDR", ":8930"),
		APIKey:             envString("API_KEY", ""),
		IMAPAddr:           envString("BRIDGE_IMAP_ADDR", "127.0.0.1:1143"),
		SMTPAddr:           envString("BRIDGE_SMTP_ADDR", "127.0.0.1:1025"),
		Username:           envString("BRIDGE_USERNAME", ""),
		Password:           envString("BRIDGE_PASSWORD", ""),
		FromName:           envString("MAIL_FROM_NAME", ""),
		MaxBodyChars:       envInt("MAX_BODY_CHARS", 20000),
		DefaultSearchLimit: envInt("SEARCH_DEFAULT_LIMIT", 20),
		MaxSearchLimit:     envInt("SEARCH_MAX_LIMIT", 100),
	}
	cfg.FromAddress = envString("MAIL_FROM_ADDRESS", cfg.Username)

	var err error
	if cfg.TLSSkipVerify, err = envBool("BRIDGE_TLS_SKIP_VERIFY", true); err != nil {
		return cfg, err
	}
	if cfg.AllowSend, err = envBool("ALLOW_SEND", true); err != nil {
		return cfg, err
	}
	if cfg.AllowDelete, err = envBool("ALLOW_DELETE", true); err != nil {
		return cfg, err
	}
	if cfg.AllowPurge, err = envBool("ALLOW_PURGE", false); err != nil {
		return cfg, err
	}

	if cfg.Username == "" || cfg.Password == "" {
		return cfg, fmt.Errorf("BRIDGE_USERNAME and BRIDGE_PASSWORD are required")
	}
	if cfg.MaxSearchLimit < cfg.DefaultSearchLimit {
		return cfg, fmt.Errorf("SEARCH_MAX_LIMIT must be >= SEARCH_DEFAULT_LIMIT")
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s: expected true/false, got %q", key, v)
	}
	return b, nil
}
