//go:build !linux && !darwin && !windows

package node

import "net"

// ccPluginPeerPID: not known here, so the plugin is handed nothing (ccplugin.go).
func ccPluginPeerPID(net.Conn) (int, error) { return 0, errNoPeer }
