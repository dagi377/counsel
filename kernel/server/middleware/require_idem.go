package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
)

// IdempotencyKeyRef is the OpenAPI parameter every money or external-effect operation must use.
const IdempotencyKeyRef = "#/components/parameters/IdempotencyKey"

// IdempotentOperation is one OpenAPI operation marked x-money or x-external-effect.
type IdempotentOperation struct {
	Method         string
	Path           string
	OperationID    string
	Money          bool
	ExternalEffect bool
	// HasKeyParam reports whether the operation (or its path item) references IdempotencyKeyRef.
	HasKeyParam bool
}

type oaParam struct {
	Ref string `yaml:"$ref"`
}

type oaOperation struct {
	OperationID    string    `yaml:"operationId"`
	Parameters     []oaParam `yaml:"parameters"`
	Money          bool      `yaml:"x-money"`
	ExternalEffect bool      `yaml:"x-external-effect"`
}

type oaDoc struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

var oaMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// IdempotentOperations lists every operation in an OpenAPI document marked x-money or
// x-external-effect, sorted by path then method.
// rule: ARC-I-007
func IdempotentOperations(spec []byte) ([]IdempotentOperation, error) {
	var doc oaDoc
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("middleware: parse openapi: %w", err)
	}
	var out []IdempotentOperation
	for path, item := range doc.Paths {
		var shared []oaParam
		if n, ok := item["parameters"]; ok {
			if err := n.Decode(&shared); err != nil {
				return nil, fmt.Errorf("middleware: decode %s parameters: %w", path, err)
			}
		}
		for method, n := range item {
			if !oaMethods[method] {
				continue
			}
			var op oaOperation
			if err := n.Decode(&op); err != nil {
				return nil, fmt.Errorf("middleware: decode %s %s: %w", method, path, err)
			}
			if !op.Money && !op.ExternalEffect {
				continue
			}
			out = append(out, IdempotentOperation{
				Method:         strings.ToUpper(method),
				Path:           path,
				OperationID:    op.OperationID,
				Money:          op.Money,
				ExternalEffect: op.ExternalEffect,
				HasKeyParam:    hasKeyRef(shared) || hasKeyRef(op.Parameters),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}

func hasKeyRef(ps []oaParam) bool {
	for _, p := range ps {
		if p.Ref == IdempotencyKeyRef {
			return true
		}
	}
	return false
}

// RequireIdempotencyKey rejects a POST that carries no Idempotency-Key header with 400
// problem+json. Mount it on money and external-effect routes, ahead of the idem store
// middleware, so a missing key is refused before any work starts.
// rule: ARC-I-007
func RequireIdempotencyKey() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// rule: ARC-I-007
			if r.Method == http.MethodPost && strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
				w.Header().Set("Content-Type", api.ProblemContentType)
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(api.Problem{
					Type:   "urn:counsel:problem:" + idem.CodeKeyRequired,
					Title:  "Idempotency-Key header is required",
					Status: http.StatusBadRequest,
					Code:   idem.CodeKeyRequired,
					Rule:   "ARC-I-007",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
