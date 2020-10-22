package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultServerPort = ":8080"
	writeTimeout      = 500 * time.Millisecond
	maxBufferSize     = 1024
)

func main() {

	sc := make(chan os.Signal)
	// setup shutdown handler.
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// start server loop.
	ctx, cancel := context.WithCancel(context.Background())
	go runServer(ctx, defaultServerPort)

	for {
		select {
		case s := <-sc:
			log.Printf("signal %v received, waiting for server to exit.", s)
			cancel()
			log.Printf("exiting...")
			return
		}
	}
}

func runServer(ctx context.Context, serverAddr string) error {
	pc, err := net.ListenPacket("udp", serverAddr)
	if err != nil {
		return err
	}
	log.Printf("UDP server listening on %s", defaultServerPort)
	defer pc.Close()

	doneCh := make(chan error, 1)

	go clientLoop(pc, doneCh)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-doneCh:
		return err
	}

}

func clientLoop(pc net.PacketConn, doneCh chan error) {
	buf := make([]byte, maxBufferSize)
	for {
		n, addr, err := pc.ReadFrom(buf[:])
		if err != nil {
			doneCh <- err
			return
		}

		log.Printf("packet-received: bytes=%d from=%s", n, addr.String())

		err = pc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err != nil {
			doneCh <- err
			return
		}

		n, err = pc.WriteTo(buf[:n], addr)
		if err != nil {
			doneCh <- err
			return
		}

		log.Printf("packet-written: bytes=%d to=%s", n, addr.String())
	}
}
