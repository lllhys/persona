package rpc

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
)

// A line the reader gave away (lineReader.Owned) is megabytes, nearly all of them its params or its result: an
// upload relayed inline, a large answer. json.Unmarshal copies that value out of the line into a json.RawMessage (it
// has to: it does not know the line stays), so the request held the line and a copy of nearly all of it (#3199).
// unmarshalOwned leaves the value where it is.

// kept is a value of an owned line: where the decoder read it, not a copy of it.
type kept struct {
	value []byte // as the decoder handed it on, for in to find in the line
	end   int64  // the offset in the input right after it
}

// UnmarshalJSONFrom takes the next value as json.RawMessage's UnmarshalJSON does, null included, without copying it.
func (k *kept) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	k.value, k.end = v, dec.InputOffset()
	return nil
}

// in is k's value as the part of line it is, nil for a field the line does not have. False when the decoder did not
// hand on the line's own bytes: the value may be in a buffer of the decoder's, gone by now. A value that is less than
// half the line is copied after all: whoever keeps it then keeps it alone, not megabytes of a line around it.
func (k kept) in(line []byte) (json.RawMessage, bool) {
	if k.value == nil {
		return nil, true
	}
	n := int64(len(k.value))
	from := k.end - n
	if n == 0 || from < 0 || k.end > int64(len(line)) || &k.value[0] != &line[from] {
		return nil, false
	}
	if 2*n < int64(len(line)) {
		return bytes.Clone(line[from:k.end]), true
	}
	return line[from:k.end:k.end], true // capped: appending to it never writes into the line
}

// ownedMessage is message, read with its params and its result left in the line.
type ownedMessage struct {
	ID     string `json:"id,omitempty"`
	Method string `json:"method,omitempty"`
	Event  string `json:"event,omitempty"`
	Params kept   `json:"params,omitempty"`
	Result kept   `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// unmarshalOwned is json.Unmarshal(line, m) for a line the caller owns and m at its zero value, with m.Params or
// m.Result a part of line instead of a copy when it is most of the line. False: it did not read the line that way (not
// valid JSON, not a message, or a decoder that hands on other bytes than the line's), m is untouched and json.Unmarshal
// says what the line is.
// TestUnmarshalOwnedIsUnmarshal and FuzzUnmarshalOwnedIsUnmarshal compare the two.
func unmarshalOwned(line []byte, m *message) bool {
	var o ownedMessage
	if err := json.Unmarshal(line, &o); err != nil {
		return false
	}
	params, ok := o.Params.in(line)
	if !ok {
		return false
	}
	result, ok := o.Result.in(line)
	if !ok {
		return false
	}
	*m = message{ID: o.ID, Method: o.Method, Event: o.Event, Params: params, Result: result, Error: o.Error}
	return true
}
