//go:build wasip1

package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

// Buffers handed to Matinee are kept here until it's done with them, so
// the garbage collector doesn't take them back.
var pinned = map[uint32][]byte{}

func pin(b []byte) uint32 {
	if len(b) == 0 {
		b = make([]byte, 1)
	}
	p := uint32(uintptr(unsafe.Pointer(&b[0])))
	pinned[p] = b
	return p
}

//go:wasmexport matinee_malloc
func malloc(size uint32) uint32 { return pin(make([]byte, size)) }

//go:wasmexport matinee_free
func free(ptr uint32) { delete(pinned, ptr) }

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// handle answers one call from Matinee. Its result is the address of the
// JSON answer in the high 32 bits and its length in the low ones.
//
//go:wasmexport matinee_handle
func handle(ptr, size uint32) uint64 {
	in := pinned[ptr][:size]
	delete(pinned, ptr)
	var res response
	if result, err := dispatch(in); err != nil {
		res.Error = err.Error()
	} else {
		res.Result = result
	}
	out, err := json.Marshal(res)
	if err != nil {
		out, _ = json.Marshal(response{Error: err.Error()})
	}
	return uint64(pin(out))<<32 | uint64(len(out))
}

var errNotHandled = errors.New("the plugin doesn't handle this")

func dispatch(in []byte) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin panicked: %v", r)
		}
	}()
	var req request
	if err := json.Unmarshal(in, &req); err != nil {
		return nil, err
	}
	switch req.Method {
	case "scan.index":
		if handlers.Index == nil {
			return nil, errNotHandled
		}
		var r IndexRequest
		if err := json.Unmarshal(req.Params, &r); err != nil {
			return nil, err
		}
		return handlers.Index(r)
	case "metadata.lookup":
		if handlers.Lookup == nil {
			return nil, errNotHandled
		}
		var r LookupRequest
		if err := json.Unmarshal(req.Params, &r); err != nil {
			return nil, err
		}
		return handlers.Lookup(r)
	case "event":
		if handlers.Event == nil {
			return nil, nil
		}
		var e Event
		if err := json.Unmarshal(req.Params, &e); err != nil {
			return nil, err
		}
		return nil, handlers.Event(e)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

//go:wasmimport matinee log
func hostLog(level uint32, ptr unsafe.Pointer, size uint32)

//go:wasmimport matinee fetch
func hostFetch(ptr unsafe.Pointer, size uint32) uint32

//go:wasmimport matinee take
func hostTake(ptr unsafe.Pointer)

func logAt(level uint32, msg string) {
	if msg == "" {
		return
	}
	b := []byte(msg)
	hostLog(level, unsafe.Pointer(&b[0]), uint32(len(b)))
}

func fetch(req Request) (*Response, error) {
	in, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	n := hostFetch(unsafe.Pointer(&in[0]), uint32(len(in)))
	out := make([]byte, n)
	if n > 0 {
		hostTake(unsafe.Pointer(&out[0]))
	}
	var res wireResponse
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return &res.Response, nil
}
