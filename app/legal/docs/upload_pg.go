package docs

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventUploaded is the a2 event a stored upload publishes (spec/eng/a2-interfaces.json).
const EventUploaded = "document.uploaded"

// MaxDocumentBytes is the largest file one upload stores. The base64 body the route reads is
// capped at maxUploadBytes, which this fits inside.
const MaxDocumentBytes = 20 << 20

// Uploaded is the body of EventUploaded: ids, digest, size and type only, never the bytes,
// the text or the filename.
type Uploaded struct {
	DocumentID string `json:"document_id"`
	VersionID  string `json:"version_id"`
	MatterID   string `json:"matter_id"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Mime       string `json:"mime"`
}

// NewVersion is one new document and its first version as the rows store them.
type NewVersion struct {
	Upload Upload
	Blob   kdocs.BlobRef
	Mime   string
}

// DocumentRows is the SQL of PGDocuments, one statement group per method, all in the caller's
// transaction. pgDocumentRows is the Postgres one; tests use a fake.
type DocumentRows interface {
	// ByKey returns the document by created with key and its current version; ok is false
	// when there is none.
	ByKey(ctx context.Context, tx pgx.Tx, by, key string) (Document, kdocs.VersionID, bool, error)
	// FolderMatter returns the matter of a live folder; ErrNotFound when there is none.
	FolderMatter(ctx context.Context, tx pgx.Tx, folder FolderID) (string, error)
	// Insert stores the document, its version 1 and makes it current.
	Insert(ctx context.Context, tx pgx.Tx, v NewVersion) (Document, kdocs.VersionID, error)
	// List, Get and HasVersion read documents.
	List(ctx context.Context, tx pgx.Tx, f ListFilter) ([]Document, error)
	Get(ctx context.Context, tx pgx.Tx, id string) (Document, error)
	HasVersion(ctx context.Context, tx pgx.Tx, id string, v kdocs.VersionID) (bool, error)
}

// PGDocuments is the DocumentStore over Postgres: the bytes go to the cell bucket, the
// document and its version rows, the version's text (kernel/docs StoreText) and
// EventUploaded commit in one transaction.
type PGDocuments struct {
	DB     db.DB
	Blobs  *kdocs.Blobs
	Text   kdocs.TextWriter
	Events outbox.Emitter
	// Extractor reads the text of files that have no native text; nil marks them
	// text-unavailable.
	Extractor kdocs.Extractor
	// Rows is the SQL; nil is Postgres.
	Rows DocumentRows
}

var _ DocumentStore = (*PGDocuments)(nil)

func (s *PGDocuments) rows() DocumentRows {
	if s.Rows != nil {
		return s.Rows
	}
	return pgDocumentRows{}
}

// textBuffer is a TextWriter that holds the text step's outcome until the version exists, so
// the extraction runs before the transaction opens.
type textBuffer struct {
	body   []byte
	reason string
}

func (b *textBuffer) Put(_ context.Context, _ pgx.Tx, _ kdocs.VersionID, body []byte) error {
	b.body = append([]byte(nil), body...)
	return nil
}

func (b *textBuffer) MarkUnavailable(_ context.Context, _ pgx.Tx, _ kdocs.VersionID, reason string) error {
	b.reason = reason
	return nil
}

func (b *textBuffer) apply(ctx context.Context, tx pgx.Tx, w kdocs.TextWriter, v kdocs.VersionID) error {
	if b.reason != "" {
		return w.MarkUnavailable(ctx, tx, v, b.reason)
	}
	return w.Put(ctx, tx, v, b.body)
}

// mimeOf is the media type of a file by its extension.
func mimeOf(filename string) string {
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(filename))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// Upload stores the first version of a new document, files it in its matter folder, stores
// its text and publishes EventUploaded once. The same creator and Idempotency-Key return the
// document already stored, with no second version or event.
func (s *PGDocuments) Upload(ctx context.Context, u Upload) (Document, kdocs.VersionID, error) {
	if s.DB == nil || s.Blobs == nil || s.Text == nil || s.Events == nil {
		return Document{}, "", fmt.Errorf("upload: %w", ErrNoWritePoint)
	}
	if strings.TrimSpace(u.MatterID) == "" || u.FolderID == "" || strings.TrimSpace(u.By) == "" ||
		len(u.Content) == 0 || len(u.Content) > MaxDocumentBytes {
		return Document{}, "", fmt.Errorf("upload: %w", ErrInvalid)
	}
	if !knownKind(u.Kind) {
		return Document{}, "", fmt.Errorf("upload: %w", ErrUnknownKind)
	}
	key := strings.TrimSpace(u.IdempotencyKey)
	rows := s.rows()
	// A retry is answered before any byte is stored or any model call is made.
	if key != "" {
		var d Document
		var v kdocs.VersionID
		var ok bool
		err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			d, v, ok, err = rows.ByKey(ctx, tx, u.By, key)
			return err
		})
		if err != nil {
			return Document{}, "", fmt.Errorf("upload: %w", err)
		}
		if ok {
			return d, v, nil
		}
	}
	// rule: ARC-U-012
	// The bytes live in the cell bucket; the rows hold its key, digest and size only.
	ref, err := s.Blobs.Put(ctx, bytes.NewReader(u.Content))
	if err != nil {
		return Document{}, "", fmt.Errorf("upload: %w", err)
	}
	// The text step runs before the transaction, so a model call never holds it open.
	text := &textBuffer{}
	if _, err := kdocs.StoreText(ctx, nil, text, s.Extractor, "", u.Filename, u.Content); err != nil {
		return Document{}, "", fmt.Errorf("upload: %w", err)
	}
	var d Document
	var v kdocs.VersionID
	err = s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		matter, err := rows.FolderMatter(ctx, tx, u.FolderID)
		if err != nil {
			return err
		}
		if matter != u.MatterID {
			return ErrWrongMatter
		}
		d, v, err = rows.Insert(ctx, tx, NewVersion{Upload: u, Blob: ref, Mime: mimeOf(u.Filename)})
		if err != nil {
			return err
		}
		if err := text.apply(ctx, tx, s.Text, v); err != nil {
			return err
		}
		// rule: ARC-I-030
		// The event commits with the rows: one upload, one event.
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectKind, ID: d.ID}, EventUploaded, Uploaded{
			DocumentID: d.ID, VersionID: string(v), MatterID: d.MatterID, SHA256: ref.SHA256, Bytes: ref.Size,
			Mime: mimeOf(u.Filename)})
	})
	if errors.Is(err, errDuplicateKey) && key != "" {
		// A concurrent retry won the insert: answer with its document.
		var ok bool
		err = s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			d, v, ok, err = rows.ByKey(ctx, tx, u.By, key)
			if err == nil && !ok {
				err = ErrNotFound
			}
			return err
		})
	}
	if err != nil {
		return Document{}, "", fmt.Errorf("upload: %w", err)
	}
	return d, v, nil
}

// List returns up to f.Limit documents matching f, ordered by id.
func (s *PGDocuments) List(ctx context.Context, f ListFilter) ([]Document, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("list documents: %w", ErrNoWritePoint)
	}
	var out []Document
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.rows().List(ctx, tx, f)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return out, nil
}

// Document returns one live document; ErrNotFound when there is none.
func (s *PGDocuments) Document(ctx context.Context, id string) (Document, error) {
	if s.DB == nil {
		return Document{}, fmt.Errorf("document: %w", ErrNoWritePoint)
	}
	var d Document
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		d, err = s.rows().Get(ctx, tx, id)
		return err
	})
	if err != nil {
		return Document{}, fmt.Errorf("document %s: %w", id, err)
	}
	return d, nil
}

// HasVersion reports whether v is a version of document id.
func (s *PGDocuments) HasVersion(ctx context.Context, id string, v kdocs.VersionID) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("has version: %w", ErrNoWritePoint)
	}
	var ok bool
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ok, err = s.rows().HasVersion(ctx, tx, id, v)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("has version: %w", err)
	}
	return ok, nil
}

// errDuplicateKey is a unique violation on the upload idempotency index.
var errDuplicateKey = errors.New("docs: duplicate upload key")

// pgDocumentRows is DocumentRows over the docs schema.
type pgDocumentRows struct{}

const (
	byKeySQL = `SELECT id::text, coalesce(matter_id::text, ''), doc_type, coalesce(folder_id::text, ''),
  coalesce(current_version_id::text, '')
  FROM docs.document WHERE created_by = $1::uuid AND idempotency_key = $2`
	folderMatterSQL = `SELECT coalesce(matter_id::text, '') FROM docs.folder WHERE id = $1::uuid AND archived_at IS NULL`
	// An upload is a privileged draft of the firm's matter file, never client visible: it is
	// held for the firm's records only until a release gate says otherwise.
	insertDocumentSQL = `INSERT INTO docs.document (matter_id, area, folder_id, title, doc_type, source, privilege,
  client_visible, owner_id, created_by, updated_by, idempotency_key)
  VALUES ($1::uuid, 'matter', $2::uuid, $3, $4, 'upload', 'privileged', false, $5::uuid, $5::uuid, $5::uuid,
  NULLIF($6, ''))
  RETURNING id::text`
	insertVersionSQL = `INSERT INTO docs.document_version (document_id, number, sha256, storage_key, mime, size_bytes, author_id)
  VALUES ($1::uuid, 1, $2, $3, $4, $5, $6::uuid) RETURNING id::text`
	currentVersionSQL = `UPDATE docs.document SET current_version_id = $2::uuid WHERE id = $1::uuid`
	listDocumentsSQL  = `SELECT id::text, coalesce(matter_id::text, ''), doc_type, coalesce(folder_id::text, '')
  FROM docs.document
  WHERE archived_at IS NULL AND ($1 = '' OR id > $1::uuid) AND ($2 = '' OR matter_id = $2::uuid)
    AND ($3 = '' OR doc_type = $3)
  ORDER BY id LIMIT $4`
	getDocumentSQL = `SELECT id::text, coalesce(matter_id::text, ''), doc_type, coalesce(folder_id::text, '')
  FROM docs.document WHERE id = $1::uuid AND archived_at IS NULL`
	hasVersionSQL = `SELECT EXISTS (SELECT 1 FROM docs.document_version WHERE id = $2::uuid AND document_id = $1::uuid)`
)

func (pgDocumentRows) ByKey(ctx context.Context, tx pgx.Tx, by, key string) (Document, kdocs.VersionID, bool, error) {
	var d Document
	var folder, v string
	err := tx.QueryRow(ctx, byKeySQL, by, key).Scan(&d.ID, &d.MatterID, &d.Kind, &folder, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, "", false, nil
	}
	if err != nil {
		return Document{}, "", false, fmt.Errorf("document by key: %w", err)
	}
	d.FolderID = FolderID(folder)
	return d, kdocs.VersionID(v), true, nil
}

func (pgDocumentRows) FolderMatter(ctx context.Context, tx pgx.Tx, folder FolderID) (string, error) {
	var m string
	err := tx.QueryRow(ctx, folderMatterSQL, string(folder)).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("folder %s: %w", folder, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("folder %s: %w", folder, err)
	}
	return m, nil
}

func (pgDocumentRows) Insert(ctx context.Context, tx pgx.Tx, nv NewVersion) (Document, kdocs.VersionID, error) {
	u := nv.Upload
	title := strings.TrimSpace(path.Base(u.Filename))
	if u.Filename == "" || title == "." || title == "/" {
		title = u.Kind
	}
	sum, err := hex.DecodeString(nv.Blob.SHA256)
	if err != nil || len(sum) != 32 {
		return Document{}, "", fmt.Errorf("insert document: digest: %w", ErrInvalid)
	}
	d := Document{MatterID: u.MatterID, Kind: u.Kind, FolderID: u.FolderID}
	err = tx.QueryRow(ctx, insertDocumentSQL, u.MatterID, string(u.FolderID), title, u.Kind, u.By, u.IdempotencyKey).
		Scan(&d.ID)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "document_upload_idempotency_uq" {
		return Document{}, "", errDuplicateKey
	}
	if err != nil {
		return Document{}, "", fmt.Errorf("insert document: %w", err)
	}
	var v string
	if err := tx.QueryRow(ctx, insertVersionSQL, d.ID, sum, nv.Blob.Key, nv.Mime, nv.Blob.Size, u.By).Scan(&v); err != nil {
		return Document{}, "", fmt.Errorf("insert version: %w", err)
	}
	if _, err := tx.Exec(ctx, currentVersionSQL, d.ID, v); err != nil {
		return Document{}, "", fmt.Errorf("current version: %w", err)
	}
	return d, kdocs.VersionID(v), nil
}

func (pgDocumentRows) List(ctx context.Context, tx pgx.Tx, f ListFilter) ([]Document, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := tx.Query(ctx, listDocumentsSQL, f.After, f.MatterID, f.Kind, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Document, error) {
		var d Document
		var folder string
		err := r.Scan(&d.ID, &d.MatterID, &d.Kind, &folder)
		d.FolderID = FolderID(folder)
		return d, err
	})
}

func (pgDocumentRows) Get(ctx context.Context, tx pgx.Tx, id string) (Document, error) {
	var d Document
	var folder string
	err := tx.QueryRow(ctx, getDocumentSQL, id).Scan(&d.ID, &d.MatterID, &d.Kind, &folder)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	d.FolderID = FolderID(folder)
	return d, err
}

func (pgDocumentRows) HasVersion(ctx context.Context, tx pgx.Tx, id string, v kdocs.VersionID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, hasVersionSQL, id, string(v)).Scan(&ok)
	return ok, err
}
