package main

import (
	"fmt"
	"log"
	"net"

	"server/handlers"

	"github.com/smallnest/epoller"
)

const (
	hostPort = ":8081"
	buffer   = 1024
)

func main() {
	ln, err := net.Listen("tcp", hostPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("Server listening on %s\n", hostPort)

	poller, err := epoller.NewPoller(128)
	if err != nil {
		panic(err)
	}
	defer poller.Close(true)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Printf("Accept error: %v", err)
				continue
			}

			if err := poller.Add(conn); err != nil {
				log.Printf("Failed to add connection to poller: %v", err)
				conn.Close()
				continue
			}

			fmt.Printf("New connection from %s\n", conn.RemoteAddr())
		}
	}()

	connections := make(map[net.Conn]struct{})

	for {
		// Wait for events on connections
		conns, err := poller.Wait(10)
		if err != nil {
			log.Printf("Wait error: %v", err)
			continue
		}

		for _, conn := range conns {
			if _, ok := connections[conn]; !ok {
				connections[conn] = struct{}{}
				go handlers.HandleTcpConnection(conn, poller)
			}
		}
	}
}
