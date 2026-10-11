//go:build integration

package schemas_test

import (
	"os"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/registry/schemas"
)

// A registry boots from the embedded schemas and seals with no spec/kernel on disk. It
// needs no database, so it does not read COUNSEL_TEST_DB.
func TestBootFromEmbeddedSchemasOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := os.Stat("spec/kernel"); err == nil {
		t.Fatal("spec/kernel visible from the temp dir")
	}
	s, err := registry.LoadSchemas(schemas.FS)
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	b := registry.NewBoot(s)
	if err := b.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
}
