package tools

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	blocksFile    = "migration_blocks.txt"
	migrationsDir = "../migrations"
	contractOwner = "contract"
	contractFile  = "0001_init.sql"
	blockWidth    = 100
)

// migrationBlock is one owner's number range, inclusive at both ends.
type migrationBlock struct {
	first, last int
	owner       string
}

var (
	blockLineRe    = regexp.MustCompile(`^(\d{4})-(\d{4}) ([a-z0-9_/]+)$`)
	migrationRe    = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)
	moduleHeaderRe = regexp.MustCompile(`^-- module: ([a-z0-9_/]+)$`)
	// Statements that are never expand-contract: a rename breaks the previous release, and
	// rows are never deleted or truncated by a migration (corrections are reversing rows).
	destructiveRe = regexp.MustCompile(`(?i)\b(RENAME\b|TRUNCATE\b|DELETE\s+FROM\b)`)
)

// parseBlocks reads migration_blocks.txt: blank lines and # comments are skipped, every
// other line is "<first>-<last> <owner>". Blocks are 100 wide (the contract block is
// 0001-0099), never overlap, and each owner has one block.
func parseBlocks(text string) ([]migrationBlock, error) {
	var blocks []migrationBlock
	owners := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := blockLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d: want \"NNNN-NNNN owner\", got %q", n, line)
		}
		first, _ := strconv.Atoi(m[1])
		last, _ := strconv.Atoi(m[2])
		b := migrationBlock{first: first, last: last, owner: m[3]}
		switch {
		case b.owner == contractOwner && (first != 1 || last != blockWidth-1):
			return nil, fmt.Errorf("line %d: contract block must be 0001-0099", n)
		case b.owner != contractOwner && (first%blockWidth != 0 || last != first+blockWidth-1):
			return nil, fmt.Errorf("line %d: block %s must be NN00-NN99", n, line)
		case owners[b.owner]:
			return nil, fmt.Errorf("line %d: owner %s has two blocks", n, b.owner)
		}
		owners[b.owner] = true
		blocks = append(blocks, b)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	if !owners[contractOwner] {
		return nil, errors.New("no contract block")
	}
	sorted := append([]migrationBlock(nil), blocks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].first < sorted[j].first })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].first <= sorted[i-1].last {
			return nil, fmt.Errorf("blocks %s and %s overlap", sorted[i-1].owner, sorted[i].owner)
		}
	}
	return blocks, nil
}

// stripSQLComments drops "--" line comments so a word in a comment never trips a check.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// checkMigration reports why the migration file name with content sql breaks the numbering
// scheme, or nil. Names that do not start with a digit (README.md, smoke.sql) are not
// migrations and are skipped.
func checkMigration(name, sql string, blocks []migrationBlock) error {
	if name == "" || name[0] < '0' || name[0] > '9' {
		return nil
	}
	m := migrationRe.FindStringSubmatch(name)
	if m == nil {
		return fmt.Errorf("%s: want NNNN_<subject>.sql", name)
	}
	num, _ := strconv.Atoi(m[1])
	firstLine, _, _ := strings.Cut(sql, "\n")
	firstLine = strings.TrimSpace(firstLine)
	isContract := strings.HasPrefix(firstLine, "-- CONTRACT")

	if name == contractFile {
		if !isContract {
			return fmt.Errorf("%s: missing CONTRACT header", name)
		}
		return nil
	}
	if strings.Contains(sql, "\n-- CONTRACT") || isContract {
		return fmt.Errorf("%s: only %s carries a CONTRACT header", name, contractFile)
	}
	h := moduleHeaderRe.FindStringSubmatch(firstLine)
	if h == nil {
		return fmt.Errorf("%s: first line must be \"-- module: <owner>\"", name)
	}
	owner := h[1]
	if owner == contractOwner {
		return fmt.Errorf("%s: the contract block holds only %s", name, contractFile)
	}
	var blk *migrationBlock
	for i := range blocks {
		if blocks[i].owner == owner {
			blk = &blocks[i]
		}
	}
	if blk == nil {
		return fmt.Errorf("%s: module %s has no block in %s", name, owner, blocksFile)
	}
	if num < blk.first || num > blk.last {
		return fmt.Errorf("%s: number %04d is outside %s block %04d-%04d", name, num, owner, blk.first, blk.last)
	}
	if d := destructiveRe.FindString(stripSQLComments(sql)); d != "" {
		return fmt.Errorf("%s: %q is not expand-contract", name, d)
	}
	return nil
}

func testBlocks(t *testing.T) []migrationBlock {
	t.Helper()
	blocks, err := parseBlocks("0001-0099 contract\n0100-0199 kernel/audit\n5500-5599 app/legal/trust\n")
	if err != nil {
		t.Fatalf("parseBlocks: %v", err)
	}
	return blocks
}

func TestParseBlocks(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{name: "valid with comments", text: "# x\n\n0001-0099 contract\n0100-0199 kernel/audit\n", wantErr: false},
		{name: "no contract block", text: "0100-0199 kernel/audit\n", wantErr: true},
		{name: "contract wrong range", text: "0001-0199 contract\n", wantErr: true},
		{name: "not 100 wide", text: "0001-0099 contract\n0100-0249 kernel/audit\n", wantErr: true},
		{name: "unaligned", text: "0001-0099 contract\n0150-0249 kernel/audit\n", wantErr: true},
		{name: "overlap", text: "0001-0099 contract\n0100-0199 kernel/audit\n0100-0199 kernel/gate\n", wantErr: true},
		{name: "owner twice", text: "0001-0099 contract\n0100-0199 kernel/audit\n0200-0299 kernel/audit\n", wantErr: true},
		{name: "bad line", text: "0001-0099 contract\n100-199 kernel/audit\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBlocks(tt.text)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBlocks err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckMigration(t *testing.T) {
	blocks := testBlocks(t)
	tests := []struct {
		name    string
		file    string
		sql     string
		wantErr bool
	}{
		{name: "contract baseline", file: "0001_init.sql", sql: "-- CONTRACT: frozen\nCREATE SCHEMA a;\n"},
		{name: "contract without header", file: "0001_init.sql", sql: "CREATE SCHEMA a;\n", wantErr: true},
		{name: "in own block", file: "0105_audit_index.sql", sql: "-- module: kernel/audit\nCREATE INDEX i ON audit.e (id);\n"},
		{name: "block edges", file: "5599_trust_col.sql", sql: "-- module: app/legal/trust\nALTER TABLE trust.t ADD COLUMN c int;\n"},
		{name: "outside own block", file: "0205_audit_index.sql", sql: "-- module: kernel/audit\nSELECT 1;\n", wantErr: true},
		{name: "other module block", file: "0150_trust_col.sql", sql: "-- module: app/legal/trust\nSELECT 1;\n", wantErr: true},
		{name: "unknown module", file: "0150_x.sql", sql: "-- module: kernel/nope\nSELECT 1;\n", wantErr: true},
		{name: "missing module header", file: "0150_x.sql", sql: "SELECT 1;\n", wantErr: true},
		{name: "second contract file", file: "0002_more.sql", sql: "-- CONTRACT: x\nSELECT 1;\n", wantErr: true},
		{name: "contract block taken", file: "0002_more.sql", sql: "-- module: contract\nSELECT 1;\n", wantErr: true},
		{name: "contract header later", file: "0101_x.sql", sql: "-- module: kernel/audit\n-- CONTRACT: x\nSELECT 1;\n", wantErr: true},
		{name: "rename", file: "0101_x.sql", sql: "-- module: kernel/audit\nALTER TABLE audit.e RENAME COLUMN a TO b;\n", wantErr: true},
		{name: "truncate", file: "0101_x.sql", sql: "-- module: kernel/audit\nTRUNCATE audit.e;\n", wantErr: true},
		{name: "delete rows", file: "0101_x.sql", sql: "-- module: kernel/audit\ndelete  from audit.e;\n", wantErr: true},
		{name: "destructive word in comment", file: "0101_x.sql", sql: "-- module: kernel/audit\n-- no RENAME here\nSELECT 1;\n"},
		{name: "bad name", file: "101_x.sql", sql: "-- module: kernel/audit\n", wantErr: true},
		{name: "upper case subject", file: "0101_X.sql", sql: "-- module: kernel/audit\n", wantErr: true},
		{name: "not a migration", file: "smoke.sql", sql: "TRUNCATE x;\n"},
		{name: "readme", file: "README.md", sql: "# x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMigration(tt.file, tt.sql, blocks)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkMigration(%s) err = %v, wantErr %v", tt.file, err, tt.wantErr)
			}
		})
	}
}

// TestMigrationBlocks checks the real tools/migration_blocks.txt and every file in
// migrations/ against the numbering scheme.
func TestMigrationBlocks(t *testing.T) {
	text, err := os.ReadFile(blocksFile)
	if err != nil {
		t.Fatalf("read %s: %v", blocksFile, err)
	}
	blocks, err := parseBlocks(string(text))
	if err != nil {
		t.Fatalf("%s: %v", blocksFile, err)
	}
	entries, err := os.ReadDir(migrationsDir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read %s: %v", migrationsDir, err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		sql, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := checkMigration(name, string(sql), blocks); err != nil {
			t.Error(err)
		}
		if m := migrationRe.FindStringSubmatch(name); m != nil {
			if prev, ok := seen[m[1]]; ok {
				t.Errorf("%s and %s share number %s", prev, name, m[1])
			}
			seen[m[1]] = name
		}
	}
}
