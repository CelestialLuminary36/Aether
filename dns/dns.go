// Package dns implements Aether's DNS subsystem.
//
// TODO(user): implement in Plan 3. It should support multiple DNS
// servers, each with a detour (which outbound to use for the query),
// and routing rules that select a server per domain. Provide a
// Resolver interface used by the Dispatcher during the Resolve action.
package dns
