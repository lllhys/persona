//go:build !linux && !darwin

package node

import "context"

func (*Node) stopCodexWriter(context.Context, codexWriter) error {
	return takeoverRefusal("unavailable")
}

func prepareCodexWriterQuit(context.Context, codexWriter) (func() error, error) {
	return nil, takeoverRefusal("unavailable")
}
