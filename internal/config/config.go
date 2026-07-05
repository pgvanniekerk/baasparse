// Package config loads the bootstrap configuration for a baasparse instance from
// environment and flags (BR-NFR-061). Everything behavioural (pipelines, rules)
// lives in PostgreSQL (BR-CFG-001); this is only what is needed to start.
package config

import (
	"flag"
	"os"
	"path/filepath"
)

// Config is the bootstrap configuration.
type Config struct {
	ListenAddr  string // management-plane HTTP listen address
	DatabaseURL string // libpq/pgx connection string
	SchemaPath  string // path to .db/schema.sql for auto-apply
	AutoMigrate bool   // apply schema.sql on startup if tables absent
	DataDir     string // root for input/in-progress/done/output dirs
	InstanceID  string // cluster instance identity (INS_INSTANCE)
	WatchEnable bool   // run the background folder watcher
	DBSchema    string // PostgreSQL schema (namespace) holding engine tables
}

// Dirs are the working directories under DataDir.
func (c Config) InputDir() string      { return filepath.Join(c.DataDir, "input") }
func (c Config) InProgressDir() string { return filepath.Join(c.DataDir, "in-progress") }
func (c Config) DoneDir() string       { return filepath.Join(c.DataDir, "done") }
func (c Config) OutputDir() string     { return filepath.Join(c.DataDir, "output") }

// Load resolves configuration from flags (which default from environment).
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("baasparse", flag.ContinueOnError)

	c := Config{}
	// The connection string, port and schema come from ~/.baasparse/.config
	// (see the settings package); these flags are optional overrides, so they
	// default empty and the caller fills them from settings when unset.
	fs.StringVar(&c.ListenAddr, "listen", "", "override listen address (default: port from ~/.baasparse/.config)")
	fs.StringVar(&c.DatabaseURL, "database-url", "", "override the stored PostgreSQL connection string")
	fs.StringVar(&c.DBSchema, "db-schema", "", "override the SQL schema (namespace) holding engine tables")
	fs.StringVar(&c.SchemaPath, "schema", env("BAASPARSE_SCHEMA", ".db/schema.sql"), "path to schema.sql for startup auto-apply")
	fs.BoolVar(&c.AutoMigrate, "auto-migrate", envBool("BAASPARSE_AUTO_MIGRATE", true), "apply schema.sql on startup if the schema is absent")
	fs.StringVar(&c.DataDir, "data-dir", env("BAASPARSE_DATA_DIR", "./data"), "root directory for input/in-progress/done/output")
	fs.StringVar(&c.InstanceID, "instance-id", env("BAASPARSE_INSTANCE_ID", defaultInstanceID()), "cluster instance identity")
	fs.BoolVar(&c.WatchEnable, "watch", envBool("BAASPARSE_WATCH", true), "run the background folder watcher (data plane)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, ok := os.LookupEnv(k); ok {
		return v == "1" || v == "true" || v == "yes"
	}
	return def
}


func defaultInstanceID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "instance-1"
}

// ResolveSchemaPath returns the given schema path if it exists, otherwise it
// looks next to the running executable (schema.sql or .db/schema.sql) — so a
// binary deployed to ~/.baasparse finds its schema without a working directory.
func ResolveSchemaPath(path string) string {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, cand := range []string{filepath.Join(dir, "schema.sql"), filepath.Join(dir, ".db", "schema.sql")} {
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}
	}
	return path
}
