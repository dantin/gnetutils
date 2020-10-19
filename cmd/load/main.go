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
	requests    uint64
	messageSize uint
	wg          sync.WaitGroup
)

// parseArgs parses command line arguments.
func client(ctx context.Context) {
	defer wg.Done()

	c, err := net.Dial("tcp", serverAddr)
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

	r := bufio.NewReader(c)

	for {
		if atomic.LoadUint64(&done) > 0 {
			return
		}

		_, err := c.Write(sendBuffer)
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
	flag.UintVar(&messageSize, "s", 1024, "message size in bytes")
	flag.StringVar(&serverAddr, "a", ":8080", "server address to connect to")
	flag.UintVar(&parallel, "p", 10, "parallel connection size")
	flag.Parse()

	// setup shutdown handler.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	ctx, cancel := context.WithCancel(context.Background())

	// run client.
	for i := uint(0); i < parallel; i++ {
		wg.Add(1)
		go client(ctx)
	}

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
