// Package routerenv defines the network contract shared by routing and diagnostics.
// Changing these values requires migrating the router configuration as well.
package routerenv

const (
	LANPrefix        = "192.168.50.0/24"
	LANAddress       = "192.168.50.1"
	LANInterface     = "br0"
	PanelAddress     = "192.168.50.1:8088"
	SOCKSAddress     = "127.0.0.1:10808"
	TunnelDNSPort    = "1053"
	TunnelDNS        = "127.0.0.1:" + TunnelDNSPort
	HelperDNS        = "127.0.0.1:10535"
	DNSPort          = "10535"
	TCPPort          = "12345"
	UDPPort          = "12346"
	Mark             = "0x1000000/0x1000000"
	RouteTable       = "100"
	AcceptedTCPPorts = DNSPort + "," + TCPPort
	LANNetwork       = "192.168.50.0"
	LANBroadcast     = "192.168.50.255"
)
