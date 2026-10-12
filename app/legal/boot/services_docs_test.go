package boot

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/dagi377/counsel/kernel/api"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// TestDocsMounts: the document routes are served over Postgres only with a cell bucket, an
// outbox emitter and the shared ports; otherwise they keep their placeholder (fail closed).
func TestDocsMounts(t *testing.T) {
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	ports := HTTPPorts{Access: denyAll{}, Cursors: codec, Problems: &server.Problems{}, Idem: idemfake.New(),
		Principal: func(*http.Request) (registry.Principal, bool) { return registry.Principal{}, false }}
	bucket := &kdocs.S3Bucket{Endpoint: "https://s3.example/cell", Region: "ca-central-1", AccessKey: "a", SecretKey: "s"}
	tests := []struct {
		name   string
		k      *kboot.Kernel
		bucket kdocs.Bucket
		want   int
	}{
		{"wired", &kboot.Kernel{DB: dbfake.New(), Emitter: outboxfake.New()}, bucket, 1},
		{"no bucket", &kboot.Kernel{DB: dbfake.New(), Emitter: outboxfake.New()}, nil, 0},
		{"no emitter", &kboot.Kernel{DB: dbfake.New()}, bucket, 0},
		{"no database", &kboot.Kernel{Emitter: outboxfake.New()}, bucket, 0},
	}
	saved := docsBucket
	t.Cleanup(func() { docsBucket = saved })
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			docsBucket = func() kdocs.Bucket { return tc.bucket }
			if got := len(docsMounts(tc.k, ports)); got != tc.want {
				t.Fatalf("mounts = %d, want %d", got, tc.want)
			}
		})
	}
}

// denyAll is an Access that allows nothing.
type denyAll struct{}

func (denyAll) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return false, nil
}
