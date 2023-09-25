package main

import (
	"fmt"
	"github.com/Baiguoshuai1/shadiaosocketio"
	"github.com/Baiguoshuai1/shadiaosocketio/websocket"
	"github.com/the-heart-rnd/awaitable"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClient_ShouldConnectAndDisconnect(t *testing.T) {
	jobs := awaitable.NewAwaitable(
		"server: client connected",
		"server: client disconnect because transport close",
	)

	stdoutChan, cleanup := startServer("server_1.js", t)
	go handleServerJobs("server", stdoutChan, jobs)

	c, err := shadiaosocketio.Dial(
		shadiaosocketio.GetUrl("localhost", 2233, false),
		*websocket.GetDefaultWebsocketTransport(),
	)
	if err != nil {
		panic(err)
	}

	c.On(shadiaosocketio.OnConnection, func(*shadiaosocketio.Channel) {
		time.Sleep(100 * time.Millisecond)
		c.Close()
	})

	remaining := jobs.Done(1 * time.Second)
	stdout := cleanup()
	if remaining != nil {
		t.Logf("Server output:\n%s", stdout)
		t.Fatalf("Expected communication was missing:\n%s", strings.Join(remaining, "\n"))
	}
}

func TestClient_ShouldSendMessages(t *testing.T) {
	jobs := awaitable.NewAwaitable(
		"server: client connected",
		"server: client sent message: hello world",
		"server: client sent message: goodbye world",
	)

	stdoutChan, cleanup := startServer("server_1.js", t)
	go handleServerJobs("server", stdoutChan, jobs)

	c, err := shadiaosocketio.Dial(
		shadiaosocketio.GetUrl("localhost", 2233, false),
		*websocket.GetDefaultWebsocketTransport(),
	)
	if err != nil {
		panic(err)
	}

	c.On(shadiaosocketio.OnConnection, func(*shadiaosocketio.Channel) {
		c.Emit("message", nil, "hello", "world")
		c.Emit("message", nil, "goodbye", "world")
	})

	remaining := jobs.Done(1 * time.Second)
	stdout := cleanup()
	if remaining != nil {
		t.Logf("Server output:\n%s", stdout)
		t.Fatalf("Expected communication was missing:\n%s", strings.Join(remaining, "\n"))
	}
}

func TestClient_ShouldReceiveMessages(t *testing.T) {
	jobs := awaitable.NewAwaitable(
		"server: client connected",
		"client received message: hello world",
		"client received message: goodbye world",
	)

	stdoutChan, cleanup := startServer("server_1.js", t)
	go handleServerJobs("server", stdoutChan, jobs)

	c, err := shadiaosocketio.Dial(
		shadiaosocketio.GetUrl("localhost", 2233, false),
		*websocket.GetDefaultWebsocketTransport(),
	)
	if err != nil {
		panic(err)
	}

	c.On(shadiaosocketio.OnMessage, func(c *shadiaosocketio.Channel, msg string) {
		jobs.Remove("client received message: " + strings.TrimSpace(msg))
	})

	remaining := jobs.Done(1 * time.Second)
	stdout := cleanup()
	if remaining != nil {
		t.Logf("Server output:\n%s", stdout)
		t.Fatalf("Expected communication was missing:\n%s", strings.Join(remaining, "\n"))
	}
}

func TestClient_ShouldReconnectWhenServerStops(t *testing.T) {
	jobs := awaitable.NewAwaitable(
		"server1: client connected",
		"server2: client connected",
		"client: onconnection",
		"client: onreconnection",
		"client: onconnection",
	)

	s1stdoutChan, s1cleanup := startServer("server_1.js", t)
	go handleServerJobs("server1", s1stdoutChan, jobs)

	c, err := shadiaosocketio.Dial(
		shadiaosocketio.GetUrl("localhost", 2233, false),
		*websocket.GetDefaultWebsocketTransport(),
	)
	if err != nil {
		panic(err)
	}

	c.Backoff = func(i int) time.Duration {
		return 500 * time.Millisecond
	}

	firstConnection := awaitable.NewAwaitable("client: onconnection")
	c.On(shadiaosocketio.OnConnection, func(c *shadiaosocketio.Channel) {
		jobs.Remove("client: onconnection")
		firstConnection.Remove("client: onconnection")
	})

	c.On(shadiaosocketio.OnReconnection, func(c *shadiaosocketio.Channel) {
		jobs.Remove("client: onreconnection")
	})

	c.On(shadiaosocketio.OnDisconnection, func(c *shadiaosocketio.Channel) {
		t.Fatalf("Client disconnected unexpectedly.")
	})

	// Wait for the first connection.
	remaining := firstConnection.Done(1 * time.Second)
	if remaining != nil {
		t.Fatalf("Client did not connect in expected time.")
	}
	// Simulate server restart after client was connected.
	s1output := s1cleanup()
	s2stdoutChan, s2cleanup := startServer("server_1.js", t)
	go handleServerJobs("server2", s2stdoutChan, jobs)

	remaining = jobs.Done(3 * time.Second)
	s2stdout := s2cleanup()
	if remaining != nil {
		t.Logf("Server1 output:\n%s", s1output)
		t.Logf("Server2 output:\n%s", s2stdout)
		t.Fatalf("Expected communication was missing:\n%s", strings.Join(remaining, "\n"))
	}
}

func TestClient_ShouldReconnectWhenServerClosesConnection(t *testing.T) {
	jobs := awaitable.NewAwaitable(
		"server: client connected",
		"client: onconnection",
		"client: onreconnection",
		"client: onconnection",
		"server: client connected",
	)

	stdoutChan, cleanup := startServer("server_2.js", t)
	go handleServerJobs("server", stdoutChan, jobs)

	c, err := shadiaosocketio.Dial(
		shadiaosocketio.GetUrl("localhost", 2233, false),
		*websocket.GetDefaultWebsocketTransport(),
	)
	if err != nil {
		panic(err)
	}

	c.Backoff = func(i int) time.Duration {
		return 500 * time.Millisecond
	}

	c.On(shadiaosocketio.OnConnection, func(c *shadiaosocketio.Channel) {
		jobs.Remove("client: onconnection")
	})

	c.On(shadiaosocketio.OnReconnection, func(c *shadiaosocketio.Channel) {
		jobs.Remove("client: onreconnection")
	})

	c.On(shadiaosocketio.OnDisconnection, func(c *shadiaosocketio.Channel) {
		t.Fatalf("Client disconnected unexpectedly.")
	})

	remaining := jobs.Done(3 * time.Second)
	stdout := cleanup()
	if remaining != nil {
		t.Logf("Server output:\n%s", stdout)
		t.Fatalf("Expected communication was missing:\n%s", strings.Join(remaining, "\n"))
	}
}

func startServer(variant string, t *testing.T) (<-chan string, func() string) {
	_, filename, _, _ := runtime.Caller(0)
	serverCmd := exec.Command("node", path.Join(path.Dir(filename), "server", variant))
	serverStdout, err := serverCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create server stdout pipe: %v", err)
	}

	if err := serverCmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	fullOutput := ""
	output := make(chan string)
	ready := make(chan bool, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := serverStdout.Read(buf)
			if err != nil {
				close(output)
				return
			}
			strOut := string(buf[:n])
			if strings.Contains(strOut, "listening...") {
				ready <- true
			}
			fullOutput += strOut
			select {
			case output <- strOut:
			default:
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(1 * time.Second):
		t.Fatalf("Server did not start in expected time.")
	}

	return output, func() string {
		if err := serverCmd.Process.Kill(); err != nil {
			t.Fatalf("Failed to kill server process: %v", err)
		}
		return fullOutput
	}
}

func handleServerJobs(prefix string, c <-chan string, jobs *awaitable.Awaitable) {
	for s := range c {
		jobs.Remove(fmt.Sprintf("%s: ", prefix) + strings.TrimSpace(s))
	}
}
