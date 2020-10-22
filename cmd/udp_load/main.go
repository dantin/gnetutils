package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	serverAddr  string
	parallel    uint
	messageSize uint
	requests    uint64

	wg sync.WaitGroup
)

// client sends message to server.
func client(ctx context.Context, serverAddr string, messageSize uint) {
	defer wg.Done()

	addr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		log.Fatalf("bad udp server address: %s", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Fatalf("error while connecting to server: %s", err)
	}
	defer conn.Close()

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

		atomic.AddUint64(&requests, 1)
	}
}

func main() {
	fs := flag.NewFlagSet("udp_load", flag.ExitOnError)
	fs.UintVar(&messageSize, "s", 1024, "Message size in bytes, default 1k bits.")
	fs.StringVar(&serverAddr, "a", ":8080", "Server address to connect to, default :8080.")
	fs.UintVar(&parallel, "p", 10, "Parallel connection size, default 10.")
	_ = fs.Parse(os.Args[1:])

	sc := make(chan os.Signal, 1)
	// setup shutdown handler.
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// run clients in parallel.
	ctx, cancel := context.WithCancel(context.Background())
	for i := uint(0); i < parallel; i++ {
		wg.Add(1)
		go client(ctx, serverAddr, messageSize)
	}

	// run stats loop.
	ticker := time.Tick(time.Second)
	oldValue := atomic.LoadUint64(&requests)
	for {
		select {
		case <-ticker:
			newValue := atomic.LoadUint64(&requests)
			log.Printf("%v req/sec", newValue-oldValue)
			oldValue = newValue
		case s := <-sc:
			log.Printf("signal %v received, waiting for all clients to exit.", s)
			cancel()
			wg.Wait()
			log.Printf("exiting...")
			return
		}
	}
}
