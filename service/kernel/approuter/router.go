package approuter

import "github.com/v2rayA/RoutingA"

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
	// MaxClass is how many classes a configuration may name. The number is a
	// bound on generated inbounds, not on rules: a rule that matches nothing
	// still costs nothing.
	MaxClass = 32
)

// ClassPort is the port of a class's local inbound. Classes are numbered from
// one, and the number is the one the generated inbound tag carries.
func ClassPort(class int) int { return classPortBase + class }

// Action is what happens to a flow a rule matched.
type Action uint8

const (
	// ActionDirect leaves the socket alone.
	ActionDirect Action = 0
	// ActionProxy hands the flow to a loopback inbound of the core, where the
	// class decides which outbound it leaves through.
	ActionProxy Action = 1
	// ActionBlock refuses the connection.
	ActionBlock Action = 2
)

// Network is the transport a rule is about. A rule written without
// network(...) applies to both, so the zero value means "either" rather than
// "neither".
type Network uint8

const (
	NetworkTCP Network = 1 << iota
	NetworkUDP
	NetworkBoth = NetworkTCP | NetworkUDP
)

// Covers reports whether a flow of the given transport is subject to a rule
// that names these networks.
func (n Network) Covers(flow Network) bool { return n == 0 || n&flow != 0 }

// IPRange is an inclusive range of IPv4 addresses in host order, the way the
// kernel compares them.
type IPRange struct {
	Lo uint32
	Hi uint32
}

// IPRange6 is an inclusive range of IPv6 addresses as four host-order words,
// most significant first, the way the kernel compares them.
type IPRange6 struct {
	Lo [4]uint32
	Hi [4]uint32
}

// destination is the address a rule is compared against. The two families are
// kept apart: a rule that names an IPv4 range must not catch an IPv6 flow whose
// address happens to be zero, and the other way round.
type destination struct {
	v4     uint32
	v6     [4]uint32
	isIPv6 bool
}

// exeKey is the identity of an executable: its file, not its name. Pad keeps
// the layout equal to the C struct, which encoding/binary would otherwise size
// differently.
type exeKey struct {
	Dev uint32
	Pad uint32
	Ino uint64
}

// PortRange is an inclusive range of destination ports.
type PortRange struct {
	Lo uint16
	Hi uint16
}

// Rule is one entry of the ordered list the engine applies: the first rule
// whose application and destination match decides.
type Rule struct {
	// Apps names the executables the rule is about. The engine resolves a
	// process to its executable file and matches there, so a rule keeps
	// applying to a binary that is replaced only if its name or path still
	// matches.
	Apps RoutingA.ProcessMatcher
	// Action is what a matching flow gets.
	Action Action
	// Class is the inbound that serves a proxy action.
	Class int
	// Network narrows the rule to TCP, UDP or both. Zero means both.
	Network Network
	// V4, V6 and Ports narrow the rule to some destinations. No addresses at
	// all means any address; a rule that names only one family does not match
	// the other.
	V4    []IPRange
	V6    []IPRange6
	Ports []PortRange
}

// Options is what the engine needs to start.
type Options struct {
	// Cgroup is the cgroup v2 subtree whose sockets are considered, normally
	// /sys/fs/cgroup.
	Cgroup string
	// ClassPorts maps a class number to the loopback port of its inbound.
	// A class named by a rule but missing here is refused rather than sent
	// somewhere else.
	ClassPorts map[int]int
	// Rules are applied in order; the first match decides.
	Rules []Rule
	// CorePid is the core process, whose own connections are never routed:
	// it is the process that dials the outbounds the classes name.
	CorePid int
	// UnknownPolicy is what happens to a connection whose program cannot be
	// identified. ActionDirect leaves it alone; ActionBlock refuses it.
	UnknownPolicy Action
}

// Stats is what the engine counted since it was attached. The first counters
// are decided in the kernel; the rest of the flow is counted by the relay.
type Stats struct {
	Connects       uint64
	ImplicitDirect uint64
	Self           uint64
	ForeignNetns   uint64
	Decide         uint64
	NoRule         uint64
	RuleDirect     uint64
	RuleProxy      uint64
	RuleBlock      uint64
	Redirected     uint64
	PortWritten    uint64
	Getpeername    uint64
	Exempt         uint64
	RulesFull      uint64
	Connects6      uint64
	UDP6Refused    uint64
	UDPDatagrams   uint64
	UDPRedirected  uint64
	UDPNoFlow      uint64
	UDPRewritten   uint64
	UDPAnswers     uint64
	Relayed        uint64
	RelayDirect    uint64
	Refused        uint64
	UDPRelayed     uint64
	UDPDirect      uint64
	UDPRefused     uint64
}