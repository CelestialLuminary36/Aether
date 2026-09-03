// Package route implements Aether's routing engine.
//
// TODO(user): implement in Plan 2. It should implement core.Router with
// two-stage matching:
//   1. First pass using domain, port, network, inbound tag, user, and
//      sniffed fields. An ActionResolve decision triggers DNS.
//   2. Second pass after DNS resolution allows GeoIP/CIDR matching.
//
// Support actions: proxy, direct, block, resolve.
package route
