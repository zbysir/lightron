package cdp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeBrowser answers every command with {"ok":true} and sends one event first.
func fakeBrowser(t *testing.T) (*Conn, chan *Msg) {
	t.Helper()
	cmdR, cmdW := io.Pipe() // we write commands to cmdW, the fake reads cmdR
	repR, repW := io.Pipe() // the fake writes replies to repW, we read repR
	events := make(chan *Msg, 4)
	c := New(cmdW, repR, func(m *Msg) { events <- m })
	go func() {
		_, _ = repW.Write([]byte(`{"method":"Target.attachedToTarget","params":{"sessionId":"s1"}}` + "\x00"))
		r := bufio.NewReader(cmdR)
		for {
			b, err := r.ReadBytes(0)
			if err != nil {
				return
			}
			var m Msg
			_ = json.Unmarshal(b[:len(b)-1], &m)
			if m.Method == "Fail.me" {
				_, _ = repW.Write([]byte(`{"id":` + itoa(m.ID) + `,"error":{"code":-32601,"message":"nope"}}` + "\x00"))
				continue
			}
			_, _ = repW.Write([]byte(`{"id":` + itoa(m.ID) + `,"result":{"ok":true}}` + "\x00"))
		}
	}()
	t.Cleanup(func() { cmdW.Close(); repW.Close() })
	return c, events
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestCallAndEvents(t *testing.T) {
	c, events := fakeBrowser(t)
	select {
	case m := <-events:
		if m.Method != "Target.attachedToTarget" {
			t.Fatalf("event: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	r, err := c.Call("s1", "Page.enable", nil)
	if err != nil || !strings.Contains(string(r), "true") {
		t.Fatalf("call: %s %v", r, err)
	}
	if _, err := c.Call("", "Fail.me", nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error reply: %v", err)
	}
}
