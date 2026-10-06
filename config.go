package goapp

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// DatabaseConfig holds the individual pieces of a PostgreSQL connection. Keeping
// them as separate fields (instead of one DSN string) means each value can be
// supplied from its own environment variable and validated independently.
type DatabaseConfig struct {
	Host     string // DB_HOST, e.g. "localhost"
	Port     string // DB_PORT, e.g. "5432"
	User     string // DB_USER
	Password string // DB_PASSWORD
	Name     string // DB_NAME
	SSLMode  string // DB_SSLMODE, e.g. "disable" | "require"
	TimeZone string // DB_TIMEZONE, e.g. "Asia/Jakarta"
}

// DSN builds the GORM postgres connection string from the struct fields. Empty
// optional fields are omitted so libpq falls back to its own defaults.
func (c DatabaseConfig) DSN() string {
	parts := make([]string, 0, 7)
	add := func(key, val string) {
		if val != "" {
			parts = append(parts, key+"="+val)
		}
	}
	add("host", c.Host)
	add("port", c.Port)
	add("user", c.User)
	add("password", c.Password)
	add("dbname", c.Name)
	add("sslmode", c.SSLMode)
	add("TimeZone", c.TimeZone)
	return strings.Join(parts, " ")
}

// DatabaseConfigFromEnv reads the DB_* environment variables into a
// DatabaseConfig, applying sensible defaults for host, port and sslmode.
func DatabaseConfigFromEnv() DatabaseConfig {
	return DatabaseConfig{
		Host:     env("DB_HOST", "localhost"),
		Port:     env("DB_PORT", "5432"),
		User:     env("DB_USER", ""),
		Password: env("DB_PASSWORD", ""),
		Name:     env("DB_NAME", ""),
		SSLMode:  env("DB_SSLMODE", "disable"),
		TimeZone: env("DB_TIMEZONE", ""),
	}
}

// ConfigDefault assembles the whole application Config from environment
// variables, so main.go can simply call goapp.Run(goapp.ConfigDefault()). Every
// knob has a sensible default:
//
//	PORT            listen address           (default ":8080")
//	API_PREFIX      route prefix             (default "/api/v1")
//	AUTO_MIGRATE    run AutoMigrate on boot  (default true)
//	JWT_SECRET      HMAC secret for JWTs     (default "", which disables auth)
//	JWT_ACCESS_TTL  access token lifetime    (default 0 -> 15m in the Signer)
//	JWT_REFRESH_TTL refresh token lifetime   (default 0 -> 7 days in the Signer)
//	LOG_FILE        JSON log file path       (default "", meaning stdout)
//	LOG_ENABLED     master logging switch    (default true; false = all off)
//	LOG_DISABLE     log types to silence     (csv, e.g. "db,adaptor"; default none)
//
// plus the DB_* variables read by DatabaseConfigFromEnv. Durations use Go's
// time.ParseDuration syntax, e.g. "15m", "168h".
func ConfigDefault() Config {
	return Config{
		DB:            DatabaseConfigFromEnv(),
		Port:          env("PORT", ":8080"),
		Prefix:        env("API_PREFIX", "/api/v1"),
		AutoMigrate:   envBool("AUTO_MIGRATE", true),
		JWTSecret:     env("JWT_SECRET", ""),
		JWTAccessTTL:  envDuration("JWT_ACCESS_TTL", 0),
		JWTRefreshTTL: envDuration("JWT_REFRESH_TTL", 0),
		LogFile:       env("LOG_FILE", ""),
		LogEnabled:    envBool("LOG_ENABLED", true),
		LogDisable:    envList("LOG_DISABLE"),
	}
}

// env returns the environment variable value or def when it is unset/empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool parses a boolean env var (1/t/true/0/f/false, case-insensitive),
// falling back to def when unset or unparseable.
func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// envList parses a comma-separated env var into a trimmed, non-empty slice.
// Returns nil when unset or empty.
func envList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envDuration parses a duration env var (time.ParseDuration syntax), falling
// back to def when unset or unparseable.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
