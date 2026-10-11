package server

import (
	"net/http"
	"strconv"
	"strings"
)

// ETag returns the strong ETag of a resource version: "<version>" with the quotes.
// rule: ARC-I-006
func ETag(version int64) string {
	return `"` + strconv.FormatInt(version, 10) + `"`
}

// SetETag sets the ETag response header for version.
func SetETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", ETag(version))
}

// CheckIfMatch checks the request's If-Match against the resource's current version.
// A missing If-Match returns ErrPreconditionRequired (428); one that names no current
// version returns ErrPreconditionFailed (412). Comparison is strong, so a weak tag never
// matches. "*" matches, since the resource exists.
// rule: ARC-I-006
func CheckIfMatch(r *http.Request, current int64) error {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		return ErrPreconditionRequired
	}
	want := ETag(current)
	seen := false
	for _, v := range values {
		for _, tag := range strings.Split(v, ",") {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			seen = true
			if tag == "*" || tag == want {
				return nil
			}
		}
	}
	if !seen {
		return ErrPreconditionRequired
	}
	return ErrPreconditionFailed
}
