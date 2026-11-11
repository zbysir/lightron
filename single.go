package lightron

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// One instance per data directory (two would fight over the browser profile): the first listens on a unix socket
// there (Windows 10 and later have them too), a second one says so on it ("second", then a line "url <link>" per link
// it was started with) and leaves. The first then runs OnURL with the links, or OnSecondInstance (default: bring a
// window up).
//
// Unless the first is quitting (its browser can take seconds to shut down): it answers "quitting <pid>", and the
// second waits for that process to be gone, then starts as the first. Otherwise it would hand over to an app on its
// way out, or start a browser on a profile the old one still holds (it hands over to the old one and exits): the
// app's icon showing up and going away.

// claimInstance: listening = we are the one; nil and true = another runs and was told.
func claimInstance(dir string, urls []string, onSecond func(urls []string), quitting func() bool, logf func(string, ...any)) (ln net.Listener, other bool) {
	sock := filepath.Join(dir, "instance.sock")
	for tries := 0; tries < 3; tries++ {
		c, err := net.DialTimeout("unix", sock, time.Second)
		if err != nil {
			break
		}
		msg := "second\n"
		for _, u := range urls {
			msg += "url " + u + "\n"
		}
		_, _ = c.Write([]byte(msg))
		if uc, ok := c.(*net.UnixConn); ok {
			_ = uc.CloseWrite()
		}
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		reply, _ := bufio.NewReader(c).ReadString('\n')
		c.Close()
		pid, ok := strings.CutPrefix(strings.TrimSpace(reply), "quitting ")
		if !ok {
			logf("lightron: the app is running: handed over to it")
			return nil, true // handed over (an older first instance doesn't answer at all)
		}
		logf("lightron: the running app is quitting (pid %s): waiting for it", pid)
		if n, err := strconv.Atoi(pid); err == nil {
			for end := time.Now().Add(20 * time.Second); time.Now().Before(end) && processAlive(n); {
				time.Sleep(100 * time.Millisecond)
			}
		}
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
			if second && quitting() {
				_, _ = c.Write([]byte("quitting " + strconv.Itoa(os.Getpid()) + "\n"))
				c.Close()
				continue
			}
			c.Close()
			if second {
				go onSecond(urls)
			}
		}
	}()
	return ln, false
}
