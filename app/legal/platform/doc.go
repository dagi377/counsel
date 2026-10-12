// Package platform holds Counsel's platform AI features (spec/ai/features.json area
// "platform") and the effects of the platform.* gate kinds.
//
// The features cover portal triage, mail filing, replies, call summaries and the other
// cross-matter communication features, and the firm platform screens (archive and retention,
// users and roles, text messaging, automation settings, matter closing, firm profile). Each
// feature file exports its feature.Def; app/legal/agent/featureset adds them to the
// feature.Set and boot binds them, so nothing registers from init().
//
// The gate effects apply approved platform.* gates whose subject lives in the platform
// schema: connectors, import and export jobs, readiness records, outbound releases, support
// access and firm-wide settings. Each effect runs inside the gate's tx, applies exactly what
// was approved, is idempotent by gate id, and refuses an agent approver and the preparer.
package platform
