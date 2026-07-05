package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/auth"
	"github.com/pgvanniekerk/baasparse/internal/config"
	"github.com/pgvanniekerk/baasparse/internal/settings"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// setConn stores the PostgreSQL connection string in ~/.baasparse/.config.
// Usage: baasparse set-conn [--url postgres://...]
func setConn(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("set-conn", flag.ContinueOnError)
	url := fs.String("url", "", "PostgreSQL connection string")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "baasparse — set connection string")
	if *url == "" {
		*url = promptRequired("PostgreSQL connection string (postgres://user:pass@host:5432/db?sslmode=disable)")
	}

	// Best-effort reachability check — save regardless (the DB may come up later).
	if st, err := store.OpenPG(ctx, *url, ""); err == nil {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if perr := st.Ping(pctx); perr != nil {
			fmt.Fprintf(os.Stderr, "  note: could not reach the database yet (%v) — saving anyway\n", perr)
		} else {
			fmt.Fprintln(os.Stderr, "  connection OK")
		}
		cancel()
		st.Close()
	}
	if err := settings.SetDatabaseURL(*url); err != nil {
		return err
	}
	p, _ := settings.ConfigPath()
	fmt.Printf("Connection string saved to %s\n", p)
	return nil
}

// setPort stores the HTTP port in ~/.baasparse/.config.
// Usage: baasparse set-port [--port 8080]
func setPort(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("set-port", flag.ContinueOnError)
	port := fs.Int("port", 0, "HTTP port the GUI/API listens on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "baasparse — set port")
	if *port == 0 {
		v := promptRequired(fmt.Sprintf("Port [%d]", settings.DefaultPort))
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("port must be a number: %q", v)
		}
		*port = n
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("port out of range: %d", *port)
	}
	if err := settings.SetPort(*port); err != nil {
		return err
	}
	fmt.Printf("Port saved: %d\n", *port)
	return nil
}

// setupDB applies the database schema and stores the connection string used.
// Usage: baasparse setupdb [--url ...] [--schema ...]
func setupDB(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setupdb", flag.ContinueOnError)
	url := fs.String("url", "", "PostgreSQL connection string (default: stored value, else prompt)")
	schema := fs.String("schema", ".db/schema.sql", "schema.sql file to apply")
	dbSchemaFlag := fs.String("db-schema", "", "SQL schema (namespace) for engine tables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, _ := settings.Resolve()

	fmt.Fprintln(os.Stderr, "baasparse — database setup")
	connStr, fromInput := resolveConn(*url, s)
	dbSchema := firstNonEmpty(*dbSchemaFlag, s.DBSchema, settings.DefaultSchema)

	schemaPath := config.ResolveSchemaPath(*schema)
	if _, err := os.Stat(schemaPath); err != nil {
		return fmt.Errorf("schema file not found (%s): %w", *schema, err)
	}

	st, err := store.OpenPG(ctx, connStr, dbSchema)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Ping(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Applying schema from %s …\n", schemaPath)
	if err := st.ApplySchema(ctx, schemaPath); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Persist the connection string so the app can start without re-supplying it —
	// but only when it came from a flag/prompt, never an environment overlay.
	saved := ""
	if fromInput {
		if err := settings.SetDatabaseURL(connStr); err != nil {
			return err
		}
		saved = " (connection saved)"
	}
	n, _ := st.CountTables(ctx)
	fmt.Printf("Schema ready: %d tables in schema %q%s. Next: baasparse createadmin\n", n, dbSchema, saved)
	return nil
}

// createAdmin provisions (or resets) an Administrator, prompting for each field
// it is not given, and hashing the password (BR-USR-004/009).
func createAdmin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("createadmin", flag.ContinueOnError)
	username := fs.String("username", "", "admin username")
	display := fs.String("display-name", "", "display name")
	email := fs.String("email", "", "email (optional)")
	password := fs.String("password", "", "admin password")
	url := fs.String("url", "", "PostgreSQL connection string (default: stored value, else prompt)")
	schema := fs.String("schema", ".db/schema.sql", "schema file to ensure applied (idempotent); empty to skip")
	dbSchemaFlag := fs.String("db-schema", "", "SQL schema (namespace) for engine tables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, _ := settings.Resolve()

	fmt.Fprintln(os.Stderr, "baasparse — create administrator")
	connStr, _ := resolveConn(*url, s)
	dbSchema := firstNonEmpty(*dbSchemaFlag, s.DBSchema, settings.DefaultSchema)

	// Prompt sequentially for any field not supplied via flags.
	if *username == "" {
		*username = prompt("Username", "admin")
	}
	if *display == "" {
		*display = prompt("Display name", "Administrator")
	}
	if *email == "" {
		*email = prompt("Email (optional)", "")
	}
	if *password == "" {
		*password = promptPassword()
	}

	st, err := store.OpenPG(ctx, connStr, dbSchema)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Ping(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if *schema != "" {
		if p := config.ResolveSchemaPath(*schema); fileExists(p) {
			if err := st.ApplySchema(ctx, p); err != nil {
				return fmt.Errorf("apply schema: %w", err)
			}
		}
	}
	hash, err := auth.HashPassword(*password)
	if err != nil {
		return err
	}
	if err := st.UpsertAdmin(ctx, *username, *display, *email, hash); err != nil {
		return err
	}
	fmt.Printf("Administrator %q is ready — sign in at the GUI.\n", *username)
	return nil
}

// resolveConn returns the connection string and whether it came from user input
// (a flag or the interactive prompt). A value sourced from settings (which may be
// an environment overlay) reports fromInput=false so callers never persist an
// env-provided secret to the config file.
func resolveConn(flagVal string, s settings.Settings) (conn string, fromInput bool) {
	if flagVal != "" {
		return flagVal, true
	}
	if s.DatabaseURL != "" {
		return s.DatabaseURL, false
	}
	return promptRequired("PostgreSQL connection string (postgres://user:pass@host:5432/db?sslmode=disable)"), true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
