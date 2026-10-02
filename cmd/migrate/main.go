// Command migrate manages the ClickHouse schema in migrations/ with
// golang-migrate. Applied versions are tracked in the schema_migrations table.
//
// Usage:
//
//	migrate [-path migrations] [-env-file .env] up        apply all pending migrations
//	migrate [-path migrations] [-env-file .env] down      roll back the last applied migration
//	migrate [-path migrations] [-env-file .env] version   print the current schema version
//
// Configuration (environment variables, optionally loaded from a dotenv file
// given by -env-file, default ./.env; see .env.example):
//
//	CLICKHOUSE_DSN   clickhouse://default:clickhouse@localhost:9000/default
//
// Migration files follow golang-migrate naming: NNN_name.up.sql and
// NNN_name.down.sql, one statement per file.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers the "clickhouse" database/sql driver used by golang-migrate
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/clickhouse"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/KhashayarKhm/go-challenge/internal/envfile"
)

const usage = `Usage: migrate [-path dir] [-env-file file] <command>

Commands:
  up        apply all pending migrations
  down      roll back the last applied migration
  version   print the current schema version

Flags:
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	path := flags.String("path", "migrations", "directory with *.up.sql and *.down.sql files")
	loadEnv := envfile.Register(flags)
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), usage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("expected exactly one command: up, down or version")
	}
	command := flags.Arg(0)
	switch command {
	case "up", "down", "version":
	default:
		flags.Usage()
		return fmt.Errorf("unknown command %q", command)
	}

	if err := loadEnv(); err != nil {
		return err
	}
	dsn := os.Getenv("CLICKHOUSE_DSN")
	if dsn == "" {
		return fmt.Errorf("\"CLICKHOUSE_DSN\" is required")
	}

	m, err := migrate.New("file://"+*path, dsn)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	defer m.Close()

	switch command {
	case "up":
		if err := m.Up(); errors.Is(err, migrate.ErrNoChange) {
			fmt.Fprintln(out, "no change: schema is up to date")
		} else if err != nil {
			return fmt.Errorf("up: %w", err)
		}
	case "down":
		if _, _, err := m.Version(); errors.Is(err, migrate.ErrNilVersion) {
			fmt.Fprintln(out, "no change: no migration applied")
			return nil
		}
		if err := m.Steps(-1); err != nil {
			return fmt.Errorf("down: %w", err)
		}
	}
	return printVersion(m, out)
}

func printVersion(m *migrate.Migrate, out io.Writer) error {
	version, dirty, err := m.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		fmt.Fprintln(out, "version: none (no migration applied)")
		return nil
	case err != nil:
		return fmt.Errorf("version: %w", err)
	case dirty:
		// A migration failed midway; golang-migrate refuses up/down until the
		// schema is fixed by hand and the version is forced.
		fmt.Fprintf(out, "version: %d (dirty)\n", version)
	default:
		fmt.Fprintf(out, "version: %d\n", version)
	}
	return nil
}
