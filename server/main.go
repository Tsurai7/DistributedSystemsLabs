package main

import (
	"fmt"
	"log"
	"time"
	"net"

	"server/handlers"
)

const (
	TcpHostPort = ":8081"
	UdpHostPort = ":9091"
	KeepAlivePeriod = 30
)

func main() {
	startTcpServer()
	startUdpServer()
	select {}
}

func startTcpServer() {
	ln, err := net.Listen("tcp", TcpHostPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
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

		go handlers.HandleTcpConnection(conn)
	}
}

func startUdpServer() {
	addr, err := net.ResolveUDPAddr("udp", UdpHostPort)
	if err != nil {
		log.Fatalf("Failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("Failed to start UDP server: %v", err)
	}
	defer conn.Close()

	fmt.Printf("UDP server listening on %s\n", UdpHostPort)
	
	go handlers.HandleUdpConnections(conn)
}
