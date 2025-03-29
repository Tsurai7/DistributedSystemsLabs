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
	tcpConnChan := make(chan net.Conn)
	errChan := make(chan error, 2)

	go func() {
		err := startTcpServer(tcpConnChan)
		errChan <- err
	}()

	go func() {
		err := startUdpServer()
		errChan <- err
	}()

	for {
		select {
		case conn := <-tcpConnChan:
			fmt.Printf("New TCP connection from %s\n", conn.RemoteAddr())
			go handlers.HandleTcpConnections(conn)

		case err := <-errChan:
			if err != nil {
				log.Fatalf("Server error: %v", err)
			}

		}
	}
}

func startTcpServer(tcpConnChan chan net.Conn) error {
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

		// Отправляем TCP соединение в главный поток для обработки
		tcpConnChan <- conn
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
