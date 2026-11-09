// Package cdp is a minimal Chrome DevTools Protocol client over --remote-debugging-pipe: JSON messages, each
// followed by a NUL byte, the browser reading fd 3 and writing fd 4.
//
// The pipe rather than --remote-debugging-port, for two reasons: no other local process can connect and drive the
// browser, and Chrome offers some domains only to a pipe client.
package cdp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Msg is one protocol message: a reply (ID set) or an event (Method set).
type Msg struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Error is a protocol error reply.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Message) }

// Conn is a connection to one browser.
type Conn struct {
	w       io.Writer
	r       *bufio.Reader
	wmu     sync.Mutex
	seq     atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan *Msg
	onEvent func(*Msg)
	closed  chan struct{}
	// Timeout bounds every Call. Default 30s.
	Timeout time.Duration
}

// New reads replies and events from r and writes commands to w. onEvent runs on the reading goroutine, in order:
// a handler that calls back into the browser must do it on a goroutine of its own.
func New(w io.Writer, r io.Reader, onEvent func(*Msg)) *Conn {
	c := &Conn{w: w, r: bufio.NewReaderSize(r, 1<<20), pending: map[int64]chan *Msg{}, onEvent: onEvent,
		closed: make(chan struct{}), Timeout: 30 * time.Second}
	go c.read()
	return c
}

// Closed is closed once the browser's end of the pipe is.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

func (c *Conn) read() {
	defer close(c.closed)
	for {
		b, err := c.r.ReadBytes(0)
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.pending {
				ch <- &Msg{Error: &Error{Message: "browser connection closed"}}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var m Msg
		if json.Unmarshal(b[:len(b)-1], &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- &m
			}
			continue
		}
		if c.onEvent != nil {
			c.onEvent(&m)
		}
	}
}

// Call sends method to the session ("" = the browser) and waits for its result.
func (c *Conn) Call(session, method string, params any) (json.RawMessage, error) {
	id := c.seq.Add(1)
	ch := make(chan *Msg, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if session != "" {
		req["sessionId"] = session
	}
	b, _ := json.Marshal(req)
	c.wmu.Lock()
	_, err := c.w.Write(append(b, 0))
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %w", method, m.Error)
		}
		return m.Result, nil
	case <-time.After(c.Timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: timeout", method)
	}
}

// Eval runs JS in a page and returns its value as JSON (promises awaited).
func (c *Conn) Eval(session, js string) (json.RawMessage, error) {
	r, err := c.Call(session, "Runtime.evaluate", map[string]any{"expression": js, "awaitPromise": true, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	var v struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	_ = json.Unmarshal(r, &v)
	if e := v.ExceptionDetails; e != nil {
		return nil, fmt.Errorf("js: %s %s", e.Text, e.Exception.Description)
	}
	return v.Result.Value, nil
}
