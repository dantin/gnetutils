package duplicator

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	version           = "0.0.1-dev"
	defaultBufferSize = 1024
)

var (
	writeTimeout   time.Duration
	connectTimeout time.Duration
	//delay          time.Duration
)

type mirror struct {
	addr   string
	conn   net.Conn
	closed uint32
}

type mirrorList []string

func (l *mirrorList) String() string {
	return fmt.Sprint(*l)
}

func (l *mirrorList) Set(value string) error {
	for _, m := range strings.Split(value, ",") {
		*l = append(*l, m)
	}
	return nil
}

// Server encapsulates a TCP duplicate server application.
type Server struct {
	args []string

	listenAddr  string
	forwardAddr string
	mirrorAddrs mirrorList

	watchStopCh chan os.Signal
}

// New returns a runnable TCP duplicate server given a command line arguments array.
func New(args []string) *Server {
	return &Server{
		args:        args,
		watchStopCh: make(chan os.Signal, 1),
	}
}

// Run runs TCP duplicate server until either a stop signal is received or an error occurs.
func (s *Server) Run() error {
	var (
		showVersion bool
		showUsage   bool
	)

	fs := flag.NewFlagSet("duplicator", flag.ExitOnError)
	fs.BoolVar(&showVersion, "v", false, "Print version information.")
	fs.BoolVar(&showUsage, "h", false, "Show help message.")
	fs.StringVar(&s.listenAddr, "l", "", "Listening address (e.g. 'localhost:8080').")
	fs.StringVar(&s.forwardAddr, "f", "", "Forward to address (e.g. 'localhost:8081').")
	fs.Var(&s.mirrorAddrs, "m", "Comma separated list of mirror addresses (e.g. 'localhost:8082,localhost:8083').")
	fs.DurationVar(&connectTimeout, "t", 500*time.Millisecond, "Mirror connect timeout")
	//fs.DurationVar(&delay, "d", 20*time.Second, "Delay connecting to mirror after unsuccessful attempt")
	fs.DurationVar(&writeTimeout, "d", 20*time.Millisecond, "Mirror write timeout")
	_ = fs.Parse(s.args[1:])

	if showVersion {
		fmt.Println(version)
		return nil
	}

	if showUsage || s.listenAddr == "" || s.forwardAddr == "" {
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
	go s.serverLoop(ctx)

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

func (s *Server) serverLoop(ctx context.Context) {
	l, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		log.Fatalf("error while listening: %s", err)
	}
	log.Printf("TCP duplicate server is listening on %s", s.listenAddr)

	var connNo uint64

	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("error while accepting: %s", err)
			continue
		}

		log.Printf("connection accepted %d (%s <-> %s)", connNo, c.RemoteAddr(), c.LocalAddr())

		go s.relay(c, connNo)

		connNo++
	}
}

func (s *Server) relay(connClient net.Conn, connNo uint64) {
	defer connClient.Close()

	connID := fmt.Sprintf("%d (%s <-> %s)", connNo, connClient.LocalAddr(), connClient.RemoteAddr())

	connForwardee, err := net.Dial("tcp", s.forwardAddr)
	if err != nil {
		log.Printf("error while connecting to forwarder (%s), will close client connection", err)
		return
	}
	defer connForwardee.Close()

	var mirrors []mirror

	for _, addr := range s.mirrorAddrs {
		connMirror, err := net.DialTimeout("tcp", addr, connectTimeout)
		if err != nil {
			log.Printf("error while connecting to mirror %s (%s), will continue", addr, err)
			continue
		}

		mirrors = append(mirrors, mirror{
			addr:   addr,
			conn:   connMirror,
			closed: 0,
		})
	}

	defer func() {
		for i, m := range mirrors {
			if closed := atomic.LoadUint32(&mirrors[i].closed); closed == 1 {
				continue
			}
			m.conn.Close()
		}
	}()

	closeCh := make(chan error, 1024)
	errorCh := make(chan error, 1024)

	connect(connClient, connForwardee, mirrors, closeCh, errorCh)

	for {
		select {
		case err := <-errorCh:
			if err != nil {
				log.Printf("got error (%s), will continue", err)
			}
		case err := <-closeCh:
			if err != nil {
				log.Printf("got error (%s), will close client connection", err)
			}
			log.Printf("connection destroyed %s", connID)
			return
		}
	}
}

func connect(origin net.Conn, forwarder net.Conn, mirrors []mirror, closeCh chan error, errorCh chan error) {
	for i := 0; i < len(mirrors); i++ {
		go readAndDiscard(mirrors[i], closeCh)
	}

	go forward(forwarder, origin, closeCh)
	go forwardAndCopy(origin, forwarder, mirrors, closeCh, errorCh)
}

func readAndDiscard(m mirror, closeCh chan error) {
	for {
		var b [defaultBufferSize]byte

		_, err := m.conn.Read(b[:])
		if err != nil {
			m.conn.Close()
			atomic.StoreUint32(&m.closed, 1)
			select {
			case closeCh <- err:
			default:
			}
			return
		}
	}
}

func forward(from net.Conn, to net.Conn, closeCh chan error) {
	for {
		var b [defaultBufferSize]byte

		n, err := from.Read(b[:])
		if err != nil {
			if err != io.EOF {
				closeCh <- fmt.Errorf("from.Read() failed: %w", err)
			} else {
				closeCh <- nil
			}
			return
		}

		_, err = to.Write(b[:n])
		if err != nil {
			closeCh <- fmt.Errorf("to.Write() failed: %w", err)
			return
		}
	}
}

func forwardAndCopy(from net.Conn, to net.Conn, mirrors []mirror, closeCh, errorCh chan error) {
	for {
		var b [defaultBufferSize]byte

		n, err := from.Read(b[:])
		if err != nil {
			if err != io.EOF {
				closeCh <- fmt.Errorf("from.Read() failed: %w", err)
			} else {
				closeCh <- nil
			}
			return
		}

		_, err = to.Write(b[:n])
		if err != nil {
			closeCh <- fmt.Errorf("to.Write() failed: %w", err)
			return
		}

		for i := 0; i < len(mirrors); i++ {
			if closed := atomic.LoadUint32(&mirrors[i].closed); closed == 1 {
				continue
			}

			mirrors[i].conn.SetWriteDeadline(time.Now().Add(writeTimeout))

			_, err = mirrors[i].conn.Write(b[:n])
			if err != nil {
				mirrors[i].conn.Close()
				atomic.StoreUint32(&mirrors[i].closed, 1)
				select {
				case errorCh <- err:
				default:
				}
			}
		}
	}
}
