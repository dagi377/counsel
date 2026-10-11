package docs

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/dagi377/counsel/kernel/api"
)

// CheckUpload resolves the kind of an incoming upload before any bytes are stored. An
// unregistered kind returns ErrUnknownKind, which UploadProblem maps to 422.
func CheckUpload(kinds Kinds, kind string) (Kind, error) {
	k, err := kinds.Kind(kind)
	if err != nil {
		return Kind{}, fmt.Errorf("upload: %w", err)
	}
	return k, nil
}

// UploadProblem maps an upload error to its problem+json body, or nil when err is not a
// docs upload error. The detail never carries document text or bytes.
func UploadProblem(err error) *api.Problem {
	switch {
	case errors.Is(err, ErrUnknownKind):
		return &api.Problem{
			Title:  "Unregistered document kind",
			Status: http.StatusUnprocessableEntity,
			Code:   "docs.unknown_kind",
		}
	case errors.Is(err, ErrNotFound):
		return &api.Problem{
			Title:  "Document version not found",
			Status: http.StatusNotFound,
			Code:   "docs.not_found",
		}
	}
	return nil
}
