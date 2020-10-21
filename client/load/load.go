package load

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	version = "0.0.1-dev"
)

// Client encapsulates a TCP load testing client application.
type Client struct {
	args        []string
	requests    uint64
	watchStopCh chan os.Signal
	wg          sync.WaitGroup
}

// New returns a runnable load client given a command line arguments array.
func New(args []string) *Client {
	return &Client{
		args:        args,
		watchStopCh: make(chan os.Signal, 1),
	}
}

// client sends message to server.
func (c *Client) client(ctx context.Context, serverAddr string, messageSize uint) {
	defer c.wg.Done()

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("error while connecting to server: %s", err)
	}

	var done uint64

	go func() {
		<-ctx.Done()
		atomic.AddUint64(&done, 1)
	}()

	sendBuffer := bytes.Repeat([]byte("a"), int(messageSize-1))
	sendBuffer = append(sendBuffer, '\n')

	r := bufio.NewReader(conn)

	for {
		if atomic.LoadUint64(&done) > 0 {
			return
		}

		_, err := conn.Write(sendBuffer)
		if err != nil {
			log.Fatalf("error while sending to server: %s", err)
		}

		_, err = r.ReadBytes('\n')
		if err != nil {
			log.Fatalf("error while reading from server: %s", err)
		}

		atomic.AddUint64(&c.requests, 1)
	}
}

// Run runs load testing client until either a stop signal is received or an error occurs.
func (c *Client) Run() error {
	var (
		showVersion bool
		showUsage   bool
		serverAddr  string
		parallel    uint
		messageSize uint
	)

	fs := flag.NewFlagSet("load", flag.ExitOnError)
	fs.BoolVar(&showVersion, "v", false, "Print version information.")
	fs.BoolVar(&showUsage, "h", false, "Show help message.")
	fs.UintVar(&messageSize, "s", 1024, "Message size in bytes, default 1k bits.")
	fs.StringVar(&serverAddr, "a", ":8080", "Server address to connect to, default :8080.")
	fs.UintVar(&parallel, "p", 10, "Parallel connection size, default 10.")
	_ = fs.Parse(c.args[1:])

	if showVersion {
		fmt.Println(version)
		return nil
	}

	if showUsage {
		fs.Usage()
		return nil
	}

	// setup shutdown handler.
	signal.Notify(c.watchStopCh,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// run clients in parallel.
	ctx, cancel := context.WithCancel(context.Background())
	for i := uint(0); i < parallel; i++ {
		c.wg.Add(1)
		go c.client(ctx, serverAddr, messageSize)
	}

	// run stats loop.
	ticker := time.Tick(time.Second)
	oldValue := atomic.LoadUint64(&c.requests)
	for {
		select {
		case <-ticker:
			newValue := atomic.LoadUint64(&c.requests)
			log.Printf("%v req/sec", newValue-oldValue)
			oldValue = newValue
		case s := <-c.watchStopCh:
			log.Printf("signal %v received, waiting for all clients to exit.", s)
			cancel()
			c.wg.Wait()
			log.Printf("exiting...")
			return nil
		}
	}
}
