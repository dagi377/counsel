// Package gatestest is a fake effect transaction for per-kind gate effect tests.
package gatestest

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/registry"
)

// Tx records statements. Exec answers Rows[i] rows affected for the i-th Exec (1 when
// unset); QueryRow answers the next of Scans (pgx.ErrNoRows when they run out).
type Tx struct {
	Rows  []int64
	Scans [][]any
	SQLs  []string
	Args  [][]any
	execs int
}

// Exec records sql and answers the scripted rows affected.
func (f *Tx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.SQLs, f.Args = append(f.SQLs, sql), append(f.Args, args)
	n := int64(1)
	if f.execs < len(f.Rows) {
		n = f.Rows[f.execs]
	}
	f.execs++
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

// QueryRow records sql and answers the next scripted row.
func (f *Tx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.SQLs, f.Args = append(f.SQLs, sql), append(f.Args, args)
	if len(f.Scans) == 0 {
		return row{err: pgx.ErrNoRows}
	}
	r := f.Scans[0]
	f.Scans = f.Scans[1:]
	return row{vals: r}
}

// Ran reports whether any recorded statement contains s.
func (f *Tx) Ran(s string) bool {
	for _, q := range f.SQLs {
		if strings.Contains(q, s) {
			return true
		}
	}
	return false
}

type row struct {
	vals []any
	err  error
}

func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("gatestest: scan %d values into %d", len(r.vals), len(dest))
	}
	for i, d := range dest {
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.vals[i]))
	}
	return nil
}

// Principals used across effect tests.
var (
	Lawyer = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000a"}
	Second = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000c"}
	Prep   = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000b"}
	Agent  = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: Prep.UserID}
)

// Now is the fixed effect time.
func Now() time.Time { return time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC) }

// Gate is an approved gate of kind with payload, prepared by prep and approved by approvers.
func Gate(kind, payload string, prep registry.Principal, approvers ...registry.Principal) registry.Approved {
	return registry.Approved{GateID: "00000000-0000-0000-0000-0000000000f1", Kind: kind, Payload: []byte(payload),
		Preparer: prep, Approvers: approvers}
}
