//go:build linux || darwin

package node

import "net"

// ccPluginPeerPID is the process at the other end of a node socket connection (peer_linux.go, peer_darwin.go).
func ccPluginPeerPID(c net.Conn) (int, error) { return peerPID(c) }
