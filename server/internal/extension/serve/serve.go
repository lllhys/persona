// Package serve runs a program on the process protocol (docs/extensions/extensions.md "Process extension protocol")
// for the conformance kit (extension/conformance) and the hub's tests. It wraps the official Go SDK (sdk/go/ext), the
// same code a third party's extension uses, in the hub's own types for initialize and its rpc errors. The built-in
// WeChat program uses the SDK directly (server/plugins/wechat).
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/nwverse/persona/sdk/go/ext"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Handler answers the calls of one namespace: method is without the namespace ("status" for connector.status),
// params as sent (with "contribution").
type Handler = ext.Handler

// Program is an extension program.
type Program struct {
	ID, Version string
	// Manifest is its persona-extension.json, answered to manifest (an extension elsewhere must have it; one the
	// hub starts from its package may leave it out).
	Manifest json.RawMessage
	// Init is called once, with what the hub sent; it says what the program contributes this run (a subset of its
	// manifest's), and may start the program's own work with ctx (which ends with the program).
	Init func(ctx context.Context, p extension.InitializeParams, hub *Hub) (contributes map[string]any, err error)
	// Namespaces are the handlers by namespace ("connector").
	Namespaces map[string]Handler
}

// Hub is the hub, for a program's calls and events.
type Hub struct{ h *ext.Hub }

// Call calls a method of the hub's (connector.secrets.load, …); its errors are the hub's *rpc.Error.
func (h *Hub) Call(ctx context.Context, method string, params, out any) error {
	return rpcError(h.h.Call(ctx, method, params, out))
}

// Notify sends the hub an event (connector.changed, …).
func (h *Hub) Notify(event string, params any) error { return rpcError(h.h.Notify(event, params)) }

// Serve serves prog on in / out until the hub closes them (the program should then exit) or ctx ends.
func Serve(ctx context.Context, in io.Reader, out io.Writer, prog Program) error {
	p := ext.Program{ID: prog.ID, Version: prog.Version, Manifest: prog.Manifest, Namespaces: prog.Namespaces}
	if prog.Init != nil {
		p.Init = func(ctx context.Context, in *ext.Init, hub *ext.Hub) (map[string]any, error) {
			var params extension.InitializeParams
			if err := json.Unmarshal(in.Raw, &params); err != nil {
				return nil, err
			}
			return prog.Init(ctx, params, &Hub{h: hub})
		}
	}
	return ext.Serve(ctx, in, out, p)
}

func rpcError(err error) error {
	var e *ext.Error
	switch {
	case errors.As(err, &e):
		return &rpc.Error{Message: e.Message, Code: e.Code, Data: e.Data}
	case errors.Is(err, ext.ErrClosed):
		return rpc.ErrClosed
	}
	return err
}

// NoContribution is the error for a call to a contribution the program does not have.
func NoContribution(id string) error {
	return &rpc.Error{Code: extension.CodeNoContribution, Message: "no contribution " + id}
}

// Contribution reads the contribution a call is for ("" when the call did not say).
func Contribution(params json.RawMessage) string { return ext.Contribution(params) }

// UnknownMethod is the error for a method the namespace does not have.
func UnknownMethod(method string) error {
	return &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method " + method}
}
