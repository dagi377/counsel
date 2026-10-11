package migrations

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// contractMark marks a destructive file ("-- contract: <release>", the release that stopped
// using what it drops). tools/migration_lint.py allows destructive statements only there.
var contractMark = regexp.MustCompile(`(?mi)^--[ \t]*contract:[ \t]*\S+[ \t]*$`)

// IsContract reports whether the named migration is marked "-- contract: <release>".
func IsContract(name string) (bool, error) {
	sql, err := Read(name)
	if err != nil {
		return false, err
	}
	return contractMark.Match(sql), nil
}

// Split returns the embedded migrations in apply order, divided into expand files and
// contract files.
func Split() (expand, contract []string, err error) {
	for _, name := range Names() {
		c, err := IsContract(name)
		if err != nil {
			return nil, nil, err
		}
		if c {
			contract = append(contract, name)
		} else {
			expand = append(expand, name)
		}
	}
	return expand, contract, nil
}

// ApplyExpand runs every expand (non-contract) file not yet recorded in schema_meta.applied,
// in file-name order. Contract files are left for ApplyContract.
func ApplyExpand(ctx context.Context, conn *pgx.Conn) error {
	expand, _, err := Split()
	if err != nil {
		return err
	}
	return applyNames(ctx, conn, expand)
}

// ApplyContract runs only the files marked "-- contract:" that are not yet recorded, in
// file-name order. Every expand file must already be applied, so a contract never runs on a
// database that has not finished expanding.
func ApplyContract(ctx context.Context, conn *pgx.Conn) error {
	expand, contract, err := Split()
	if err != nil {
		return err
	}
	if err := ensureTable(ctx, conn); err != nil {
		return err
	}
	done, err := applied(ctx, conn)
	if err != nil {
		return err
	}
	for _, name := range expand {
		if !done[name] {
			return fmt.Errorf("contract before expand: %s is not applied: %w", name, ErrExpandPending)
		}
	}
	return applyNames(ctx, conn, contract)
}

func ensureTable(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.PgConn().Exec(ctx, ensureVersionTable).ReadAll(); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	return nil
}

// applyNames runs the listed files that are not yet recorded, in the given order.
func applyNames(ctx context.Context, conn *pgx.Conn, names []string) error {
	if err := ensureTable(ctx, conn); err != nil {
		return err
	}
	done, err := applied(ctx, conn)
	if err != nil {
		return err
	}
	for _, name := range names {
		if done[name] {
			continue
		}
		sql, err := Read(name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, conn, name, string(sql)); err != nil {
			return err
		}
	}
	return nil
}
