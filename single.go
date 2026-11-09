package lightron

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"time"
)

// One instance per data directory (two would fight over the browser profile): the first listens on a unix socket
// there (Windows 10 and later have them too), a second one says so on it and leaves. The first then runs
// OnSecondInstance (default: bring a window up).

// claimInstance: listening = we are the one; nil and true = another runs and was told.
func claimInstance(dir string, onSecond func()) (ln net.Listener, other bool) {
	sock := filepath.Join(dir, "instance.sock")
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		_, _ = c.Write([]byte("second\n"))
		c.Close()
		return nil, true
	}
	_ = os.Remove(sock) // a stale one: nothing answered on it
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, false // carry on without: a second instance then starts a browser that refuses the profile
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			c.Close()
			if line == "second\n" {
				go onSecond()
			}
		}
	}()
	return ln, false
}
