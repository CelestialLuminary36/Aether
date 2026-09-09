// Package route implements Aether's routing engine.
//
// It implements core.Router with two-stage matching:
//  1. First pass using domain, port, network, inbound tag, user, and
//     sniffed fields. An ActionResolve decision triggers DNS.
//  2. Second pass after DNS resolution allows GeoIP/CIDR matching.
//
// Supported actions: proxy, direct, block, resolve.
package route

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/CelestialLuminary36/Aether/core"
	geoip "github.com/oschwald/geoip2-golang"
)

type Rule struct {
	DomainSuffix    []string
	Domain          []string
	DomainKeyword   []string
	Ports           []uint16
	Network         []core.Network
	InboundTag      []string
	User            []string
	SniffedProtocol []string
	GeoIP           []string
	CIDR            []netip.Prefix
	Action          core.Action
	Outbound        string
}

type Router struct {
	rules       []Rule
	defaultRule core.Decision
	geoIP       *geoip.Reader
}

func New(rules []Rule, defaultDecision core.Decision, geoIP *geoip.Reader) *Router {
	copied := make([]Rule, len(rules))
	for i := range rules {
		copied[i] = rules[i]
		copied[i].Domain = lowercaseSlice(rules[i].Domain)
		copied[i].DomainSuffix = lowercaseSlice(rules[i].DomainSuffix)
		copied[i].DomainKeyword = lowercaseSlice(rules[i].DomainKeyword)
	}
	return &Router{
		rules:       copied,
		defaultRule: defaultDecision,
		geoIP:       geoIP,
	}
}

func lowercaseSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

func (r *Router) Route(md *core.Metadata, stage core.RouteStage) (core.Decision, error) {
	// In StagePostResolve, GeoIP/CIDR rules may match and ActionResolve
	// rules are skipped to avoid infinite loops. The stage is passed
	// explicitly by the Dispatcher, never inferred from ResolvedIPs.
	return r.routeInternal(md, stage == core.StagePostResolve)
}

func (r *Router) routeInternal(md *core.Metadata, resolved bool) (core.Decision, error) {
	for _, rule := range r.rules {
		if !r.matchStage(rule, md, resolved) {
			continue
		}
		switch rule.Action {
		case core.ActionResolve:
			if resolved {
				continue
			}
			return core.Decision{Action: core.ActionResolve}, nil
		case core.ActionProxy, core.ActionDirect, core.ActionBlock:
			return core.Decision{Action: rule.Action, Outbound: rule.Outbound}, nil
		}
	}

	if r.defaultRule.Action == core.ActionResolve && resolved {
		// Second pass cannot resolve again; fall back to direct to avoid a loop.
		return core.Decision{Action: core.ActionDirect}, nil
	}
	return r.defaultRule, nil
}

func (r *Router) matchStage(rule Rule, md *core.Metadata, resolved bool) bool {
	if !matchNetwork(rule.Network, md.Network) {
		return false
	}
	if !matchPorts(rule.Ports, md.Destination.Port()) {
		return false
	}
	if !matchInboundTag(rule.InboundTag, md.InboundTag) {
		return false
	}
	if !matchUser(rule.User, md.User) {
		return false
	}
	if !matchSniffed(rule.SniffedProtocol, md.SniffedProtocol) {
		return false
	}

	if len(rule.Domain) > 0 || len(rule.DomainSuffix) > 0 || len(rule.DomainKeyword) > 0 {
		// Sniffed domain (e.g. TLS SNI) takes precedence over the
		// Destination host, because the client may connect to an IP while
		// requesting a specific hostname.
		host := md.SniffedDomain
		if host == "" {
			host = md.Destination.Domain()
		}
		if !matchDomain(rule, host) {
			return false
		}
	}

	if resolved {
		if !matchGeoIP(r.geoIP, rule.GeoIP, md.ResolvedIPs) {
			return false
		}
		if !matchCIDR(rule.CIDR, md.ResolvedIPs) {
			return false
		}
	} else {
		if len(rule.GeoIP) > 0 || len(rule.CIDR) > 0 {
			return false
		}
	}

	return true
}

func matchNetwork(rulesNetwork []core.Network, network core.Network) bool {
	if len(rulesNetwork) == 0 {
		return true
	}
	return slices.Contains(rulesNetwork, network)
}

func matchPorts(ports []uint16, port uint16) bool {
	if len(ports) == 0 {
		return true
	}
	return slices.Contains(ports, port)
}

func matchInboundTag(inboundTags []string, tag string) bool {
	if len(inboundTags) == 0 {
		return true
	}
	return slices.Contains(inboundTags, tag)
}

func matchUser(users []string, user string) bool {
	if len(users) == 0 {
		return true
	}
	return slices.Contains(users, user)
}

func matchSniffed(sniffedProtocols []string, sniffed string) bool {
	if len(sniffedProtocols) == 0 {
		return true
	}
	return slices.Contains(sniffedProtocols, sniffed)
}

func matchDomain(rule Rule, host string) bool {
	if host == "" {
		return false
	}

	lower := strings.ToLower(host)

	if slices.Contains(rule.Domain, lower) {
		return true
	}
	for _, suffix := range rule.DomainSuffix {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	for _, kw := range rule.DomainKeyword {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func matchGeoIP(reader *geoip.Reader, countries []string, ips []netip.Addr) bool {
	if len(countries) == 0 {
		return true
	}
	if len(ips) == 0 {
		return false
	}

	for _, ip := range ips {
		for _, country := range countries {
			if strings.EqualFold(country, "private") && isPrivateOrLocalIP(ip) {
				return true
			}
		}

		if reader == nil {
			continue
		}

		record, err := reader.Country(ip.AsSlice())
		if err != nil {
			continue
		}
		countryCode := strings.ToLower(record.Country.IsoCode)

		for _, country := range countries {
			if strings.EqualFold(countryCode, country) {
				return true
			}
		}
	}
	return false
}

func isPrivateOrLocalIP(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast()
}

func matchCIDR(prefixes []netip.Prefix, ips []netip.Addr) bool {
	if len(prefixes) == 0 {
		return true
	}
	if len(ips) == 0 {
		return false
	}

	for _, ip := range ips {
		for _, p := range prefixes {
			if p.Contains(ip) {
				return true
			}
		}
	}
	return false
}
