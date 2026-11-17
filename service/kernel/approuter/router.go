package approuter

// Ports of the engine. They sit beside the ones the transparent modes already
// pin: 52345 for the dokodemo inbound, 52306 for the system-proxy SOCKS
// inbound and 52353 for the DNS module.
const (
	// RelayPort is where the kernel sends a selected executable's connects.
	RelayPort = 52346
	// classPortBase is where the first class's local inbound listens. A class
	// is one destination decision; the core hears about it as a loopback SOCKS
	// inbound, so a class is told apart by its port.
	classPortBase = 52360
)

// ClassPort is the port of a class's local inbound. Classes are numbered from
// one, and the number is the one the generated inbound tag carries.
func ClassPort(class int) int { return classPortBase + class }

// Options is what the engine needs to start.
type Options struct {
	// Cgroup is the cgroup v2 subtree whose sockets are considered, normally
	// /sys/fs/cgroup.
	Cgroup string
	// Apps names the executables to route, as the kernel reports them.
	Apps []string
	// ClassPorts maps a class number to the loopback port of its inbound.
	// A class named by a flow but missing here is refused rather than sent
	// somewhere else.
	ClassPorts map[int]int
}

// Stats is what the engine counted since it was attached.
type Stats struct {
	// Connects is every TCP connect the hook saw.
	Connects uint64
	// ImplicitDirect is connects never proxied: loopback, the unspecified
	// range, multicast, and the relay's own port.
	ImplicitDirect uint64
	// NotSelected is connects from an executable that is not in the list.
	NotSelected uint64
	// Self is the daemon's own connects.
	Self uint64
	// ForeignNetns is connects in another network namespace.
	ForeignNetns uint64
	// Redirected is connects rewritten to the relay.
	Redirected uint64
	// PortWritten is redirected connects that reached the relay's lookup table.
	PortWritten uint64
	// Getpeername is sockets that were told their original destination back.
	Getpeername uint64
	// Relayed is flows the relay handed to a class inbound.
	Relayed uint64
	// Refused is flows the relay dropped: an unknown class, a missing mapping,
	// a failed handshake.
	Refused uint64
}