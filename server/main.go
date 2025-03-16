package main

import (
	"fmt"
	"log"
	"net"
	"server/handlers"
	"time"
)

const (
	TcpHostPort     = ":8081"
	UdpHostPort     = ":9091"
	KeepAlivePeriod = 30
)

func main() {
	errChan := make(chan error, 2)

	go func() {
		err := startUdpServer()
		errChan <- err
	}()

	go func() {
		err := startTcpServer()
		errChan <- err
	}()

	for err := range errChan {
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}

func startTcpServer() error {
	ln, err := net.Listen("tcp", TcpHostPort)
	if err != nil {
		return fmt.Errorf("Failed to listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("TCP server listening on %s\n", TcpHostPort)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		tcpConn := conn.(*net.TCPConn)
		if err := tcpConn.SetKeepAlive(true); err != nil {
			log.Printf("Failed to enable Keep-Alive: %v", err)
			continue
		}

		if err := tcpConn.SetKeepAlivePeriod(KeepAlivePeriod * time.Second); err != nil {
			log.Printf("Failed to set Keep-Alive period: %v", err)
			continue
		}

		fmt.Printf("New TCP connection from %s\n", conn.RemoteAddr())

		go handlers.HandleTcpConnections(conn)
	}
}

func startUdpServer() error {
	addr, err := net.ResolveUDPAddr("udp", UdpHostPort)
	if err != nil {
		return fmt.Errorf("Failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("Failed to start UDP server: %v", err)
	}
	defer conn.Close()

	fmt.Printf("UDP server listening on %s\n", UdpHostPort)

	handlers.HandleUdpConnections(conn)

	return nil
}
