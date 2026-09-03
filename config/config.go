// Package config implements Aether's configuration loading and validation.
//
// TODO(user): implement in Plan 2. It should have three layers:
//   1. Syntax layer: YAML/JSON → AST with source positions.
//   2. Semantic layer: pure validation (reference integrity, capability
//      matching, common pitfalls) producing structured diagnostics.
//   3. Instance layer: build Inbound/Outbound/Router/DNS instances from
//      the validated config.
//
// Provide a CLI command: aether check config.yaml.
package config
