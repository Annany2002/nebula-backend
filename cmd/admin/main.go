// Command admin manages platform administrator membership through local metadata access.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out, diagnostic io.Writer) error {
	flags := flag.NewFlagSet("nebula-admin", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	path := flags.String("metadata-db", "", "Explicit path to an existing, migrated metadata SQLite database")
	userID := flags.String("user-id", "", "Existing account ID; never an email address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || *userID == "" || flags.NArg() != 1 || (flags.Arg(0) != "grant" && flags.Arg(0) != "revoke") {
		return errors.New("usage: nebula-admin --metadata-db <path> --user-id <id> grant|revoke")
	}
	absolute, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=rw&_foreign_keys=on&_busy_timeout=5000"}
	// mode=rw refuses typos instead of silently creating a new metadata database.
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		return fmt.Errorf("open existing metadata database: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to existing metadata database: %w", err)
	}
	if err = storage.SetPlatformAdmin(ctx, db, *userID, flags.Arg(0) == "grant"); err != nil {
		return fmt.Errorf("%s platform admin: %w", flags.Arg(0), err)
	}
	_, err = fmt.Fprintf(out, "Platform admin %s completed for account %q.\n", flags.Arg(0), *userID)
	return err
}
