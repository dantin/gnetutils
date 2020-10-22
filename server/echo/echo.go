package echo

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
)

const (
	version = "0.0.1-dev"
)

// Server encapsulates a TCP echo server application.
type Server struct {
	args        []string
	watchStopCh chan os.Signal
}

// New returns a runnable echo server given a command line arguments array.
func New(args []string) *Server {
	return &Server{
		args:        args,
		watchStopCh: make(chan os.Signal, 1),
	}
}

func (s *Server) clientLoop(ctx context.Context, c net.Conn, connNo uint64) {
	defer c.Close()

	var (
		done   uint64
		connID = fmt.Sprintf("%d (%s <-> %s)", connNo, c.LocalAddr(), c.RemoteAddr())
	)

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
			log.Printf("connection destroyed %s", connID)
			return
		}

		_, err = c.Write(data)
		if err != nil {
			log.Fatalf("error while writing: %s", err)
		}
	}
}

func (s *Server) serverLoop(ctx context.Context, listenAddr string) {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("error while listening: %s", listenAddr)
	}
	log.Printf("TCP server is listening on %s", listenAddr)

	var (
		done   uint64
		connNo uint64
	)

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
		log.Printf("connection accepted %d (%s <-> %s)", connNo, c.RemoteAddr(), c.LocalAddr())

		go s.clientLoop(ctx, c, connNo)

		connNo++
	}
}

// Run runs echo server until either a stop signal is received or an error occurs.
func (s *Server) Run() error {
	var (
		showVersion bool
		showUsage   bool
		listenAddr  string
	)

	fs := flag.NewFlagSet("echo", flag.ExitOnError)
	fs.BoolVar(&showVersion, "v", false, "Print version information.")
	fs.BoolVar(&showUsage, "h", false, "Show help message.")
	fs.StringVar(&listenAddr, "l", ":8080", "Listening address, default(:8080).")
	_ = fs.Parse(s.args[1:])

	if showVersion {
		fmt.Println(version)
		return nil
	}

	if showUsage {
		fs.Usage()
		return nil
	}

	// setup shutdown handler.
	signal.Notify(s.watchStopCh,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// start server loop.
	ctx, cancel := context.WithCancel(context.Background())
	go s.serverLoop(ctx, listenAddr)

	for {
		select {
		case s := <-s.watchStopCh:
			log.Printf("signal %v received, waiting for server to exit.", s)
			cancel()
			log.Printf("exiting...")
			return nil
		}
	}
}
