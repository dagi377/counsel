//go:build tools

// Package tools pins every module in the AGENTS.md allowed-dependency list in go.mod, so
// parallel tasks build against one go.mod and go.sum instead of each adding requires.
package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/riverqueue/river"
	_ "github.com/riverqueue/river/riverdriver/riverpgxv5"
	_ "github.com/santhosh-tekuri/jsonschema/v5"
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	_ "go.opentelemetry.io/otel/sdk"
	_ "go.opentelemetry.io/otel/sdk/metric"
	_ "gopkg.in/yaml.v3"
)
