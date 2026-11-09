package dns

import (
	"fmt"
	"net/netip"
	"strings"
)

// Default configuration constants.
const (
	// DefaultListenAddr is the default DNS listener address.
	DefaultListenAddr = "0.0.0.0:52353"
	// DefaultTimeout is the default query timeout in seconds.
	DefaultTimeout = 5
	// DefaultCacheSize is the default maximum number of cache entries.
	DefaultCacheSize = 4096
	// DefaultMinTTL is the default minimum TTL in seconds.
	DefaultMinTTL = 60
	// DefaultMaxTTL is the default maximum TTL in seconds.
	DefaultMaxTTL = 86400
	// MaxRateLimit is the highest per-client UDP/TCP query rate accepted in a
	// configuration. It is a sanity bound, not a policy: a listener that
	// answers faster than this is not a resolver.
	MaxRateLimit = 100000
)

// DefaultDnsListenerConfig returns a DnsListenerConfig with default values.
func DefaultDnsListenerConfig() *DnsListenerConfig {
	return &DnsListenerConfig{
		ListenAddr: DefaultListenAddr,
		Timeout:    DefaultTimeout,
	}
}

// DefaultDnsModuleConfig returns a DnsModuleConfig with default values.
func DefaultDnsModuleConfig() *DnsModuleConfig {
	return &DnsModuleConfig{
		Listener: *DefaultDnsListenerConfig(),
		Cache: CacheConfig{
			Enabled: true,
			Size:    DefaultCacheSize,
			MinTTL:  DefaultMinTTL,
			MaxTTL:  DefaultMaxTTL,
		},
	}
}

// Validate checks the DnsListenerConfig and returns an error if any field is invalid.
func (c *DnsListenerConfig) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("dns listener: listen address must not be empty")
	}
	// Basic address format check: must contain ':'
	if !strings.Contains(c.ListenAddr, ":") {
		return fmt.Errorf("dns listener: listen address %q must include port (e.g. \"0.0.0.0:52353\")", c.ListenAddr)
	}
	for i, addr := range c.ExtraListenAddrs {
		if !strings.Contains(addr, ":") {
			return fmt.Errorf("dns listener: extra address[%d] %q must include port", i, addr)
		}
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("dns listener: timeout must be positive, got %d", c.Timeout)
	}
	if c.RateLimit < 0 || c.RateLimit > MaxRateLimit {
		return fmt.Errorf("dns listener: rate limit must be between 0 and %d, got %d", MaxRateLimit, c.RateLimit)
	}
	for i, raw := range c.AllowedClients {
		if _, err := ParseClientPrefix(raw); err != nil {
			return fmt.Errorf("dns listener: allowed_clients[%d]: %w", i, err)
		}
	}
	return nil
}

// ParseClientPrefix accepts an IP or a CIDR and returns the prefix it covers.
// A bare address becomes a single-host prefix.
func ParseClientPrefix(raw string) (netip.Prefix, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return netip.Prefix{}, fmt.Errorf("empty address")
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not an IP address or CIDR", raw)
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// Validate checks the DnsModuleConfig and returns an error if any field is invalid.
func (c *DnsModuleConfig) Validate() error {
	if err := c.Listener.Validate(); err != nil {
		return err
	}
	if c.Cache.Size < 0 {
		return fmt.Errorf("dns module: cache size must be non-negative, got %d", c.Cache.Size)
	}
	if c.Cache.MinTTL < 0 {
		return fmt.Errorf("dns module: min TTL must be non-negative, got %d", c.Cache.MinTTL)
	}
	if c.Cache.MaxTTL < 0 {
		return fmt.Errorf("dns module: max TTL must be non-negative, got %d", c.Cache.MaxTTL)
	}
	if c.Cache.MinTTL > c.Cache.MaxTTL && c.Cache.MaxTTL > 0 {
		return fmt.Errorf("dns module: min TTL (%d) must not exceed max TTL (%d)", c.Cache.MinTTL, c.Cache.MaxTTL)
	}
	return nil
}

// String returns a human-readable summary of the DnsListenerConfig.
func (c *DnsListenerConfig) String() string {
	return fmt.Sprintf("listen=%s timeout=%ds", c.ListenAddr, c.Timeout)
}

// String returns a human-readable summary of the DnsModuleConfig.
func (c *DnsModuleConfig) String() string {
	cacheStatus := "disabled"
	if c.Cache.Enabled {
		cacheStatus = fmt.Sprintf("enabled(size=%d,minTTL=%d,maxTTL=%d)", c.Cache.Size, c.Cache.MinTTL, c.Cache.MaxTTL)
	}
	return fmt.Sprintf("listener[%s] cache[%s] upstreams=%d rules=%d",
		c.Listener.String(), cacheStatus, len(c.Upstreams), len(c.Rules))
}
