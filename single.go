package lightron

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// One instance per data directory (two would fight over the browser profile): the first listens on a unix socket
// there (Windows 10 and later have them too), a second one says so on it ("second", then a line "url <link>" per link
// it was started with) and leaves. The first then runs OnURL with the links, or OnSecondInstance (default: bring a
// window up).

// claimInstance: listening = we are the one; nil and true = another runs and was told.
func claimInstance(dir string, urls []string, onSecond func(urls []string)) (ln net.Listener, other bool) {
	sock := filepath.Join(dir, "instance.sock")
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		msg := "second\n"
		for _, u := range urls {
			msg += "url " + u + "\n"
		}
		_, _ = c.Write([]byte(msg))
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
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			sc := bufio.NewScanner(c)
			second, urls := false, []string(nil)
			for sc.Scan() {
				line := sc.Text()
				if line == "second" {
					second = true
				} else if u, ok := strings.CutPrefix(line, "url "); ok {
					urls = append(urls, u)
				}
			}
			c.Close()
			if second {
				go onSecond(urls)
			}
		}
	}()
	return ln, false
}
