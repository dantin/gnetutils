package tools

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	echoServerVersion = "0.0.1-dev"
)

// EchoServer encapsulates a TCP echo server application.
type EchoServer struct {
	args             []string
	shutdownWaitSecs time.Duration
	watchStopCh      chan os.Signal
}

// NewEchoServer returns a runnable EchoServer given a command line arguments array.
func NewEchoServer(args []string) *EchoServer {
	// setup shutdown handler.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	return &EchoServer{
		args:        args,
		watchStopCh: sc,
	}
}

func (es *EchoServer) clientLoop(ctx context.Context, c net.Conn) {
	defer c.Close()

	var done uint64

	go func() {
		<-ctx.Done()
		atomic.AddUint64(&done, 1)
	}()

	r := bufio.NewReader(c)
	for {
		if atomic.LoadUint64(&done) > 0 {
			return
		}

		data, err := r.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("error while reading line: %s", err)
			}
			return
		}

		_, err = c.Write(data)
		if err != nil {
			log.Fatalf("error while writing: %s", err)
		}
	}
}

func (es *EchoServer) serverLoop(ctx context.Context, listenAddr string) {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("error while listening: %s", listenAddr)
	}
	log.Printf("TCP server is listening on %s", listenAddr)

	var done uint64

	go func() {
		<-ctx.Done()
		atomic.AddUint64(&done, 1)
	}()

	for {
		if atomic.LoadUint64(&done) > 0 {
			return
		}

		c, err := l.Accept()
		if err != nil {
			log.Fatalf("error while accepting: %s", err)
		}
		log.Printf("Client connected, peer IP %s", c.RemoteAddr())
		go es.clientLoop(ctx, c)
	}
}

// Run runs EchoServer until either a stop signal is received or an error occurs.
func (es *EchoServer) Run() error {
	var (
		showVersion bool
		showUsage   bool
		listenAddr  string
	)

	fs := flag.NewFlagSet("echo", flag.ExitOnError)
	fs.BoolVar(&showVersion, "v", false, "Print version information.")
	fs.BoolVar(&showUsage, "h", false, "Show help message.")
	fs.StringVar(&listenAddr, "l", ":8080", "Listening address, default(:8080).")
	_ = fs.Parse(es.args[1:])

	if showVersion {
		fmt.Println(echoServerVersion)
		return nil
	}

	if showUsage {
		fs.Usage()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	go es.serverLoop(ctx, listenAddr)

	for {
		select {
		case s := <-es.watchStopCh:
			log.Printf("signal %v received, waiting for server to exit.", s)
			cancel()
			log.Printf("exiting...")
			return nil
		}
	}
}
