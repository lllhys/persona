// Package archguard holds only tests that read the server's source: they pin the import boundaries between the
// contract packages and the parts (the hub, the manager, the node), docs/server/architecture.md "Contract packages",
// and where the entry points reap inherited children (osproc.ReapInherited), ahead of any child they start.
package archguard
