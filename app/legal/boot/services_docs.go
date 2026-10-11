package boot

import (
	"log/slog"
	"os"

	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	kboot "github.com/dagi377/counsel/kernel/boot"
	kdocs "github.com/dagi377/counsel/kernel/docs"
)

// docsBucket is the cell document bucket. It reads the deployment's bucket settings from the
// environment; tests replace it. Nil means no bucket is configured.
var docsBucket = bucketFromEnv

// bucketFromEnv is the cell bucket from $COUNSEL_DOCS_BUCKET_URL (path-style bucket URL),
// $COUNSEL_DOCS_BUCKET_REGION, $COUNSEL_DOCS_KMS_KEY_ID and the AWS credential variables.
// Nil when the URL or a credential is missing.
func bucketFromEnv() kdocs.Bucket {
	b := &kdocs.S3Bucket{
		Endpoint:     os.Getenv("COUNSEL_DOCS_BUCKET_URL"),
		Region:       os.Getenv("COUNSEL_DOCS_BUCKET_REGION"),
		KMSKeyID:     os.Getenv("COUNSEL_DOCS_KMS_KEY_ID"),
		AccessKey:    os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretKey:    os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
	}
	if b.Endpoint == "" || b.Region == "" || b.AccessKey == "" || b.SecretKey == "" {
		return nil
	}
	return b
}

// docsMounts is GET/POST /documents and the page route over Postgres: bytes in the cell
// bucket, rows and version text in docs, document.uploaded through the kernel outbox, and the
// text step extracting non-text files through the agent model. Without a bucket, an emitter
// or the shared ports the routes keep their placeholder (503): an upload never commits
// without its bytes or its event.
func docsMounts(k *kboot.Kernel, p HTTPPorts) []mount {
	if k == nil || k.DB == nil || k.Emitter == nil || p.Idem == nil || p.Access == nil || p.Cursors == nil ||
		p.Problems == nil || p.Principal == nil {
		return nil
	}
	bucket := docsBucket()
	if bucket == nil {
		slog.Warn("document upload not served: no cell bucket configured", "env", "COUNSEL_DOCS_BUCKET_URL")
		return nil
	}
	text := kdocs.PGText{DB: k.DB}
	store := &legaldocs.PGDocuments{DB: k.DB, Blobs: kdocs.NewBlobs(bucket, nil), Text: text, Events: k.Emitter}
	if p.AgentModel != nil {
		store.Extractor = kdocs.ModelExtractor{Model: p.AgentModel, FeatureID: legaldocs.FeatureIngestDocument}
	}
	h := &legaldocs.HTTP{Documents: store, Pages: text, Access: p.Access, Cursors: p.Cursors, Idem: p.Idem,
		Problems: p.Problems, Principal: p.Principal}
	return []mount{h.Routes}
}
