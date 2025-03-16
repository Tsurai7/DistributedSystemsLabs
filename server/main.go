package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"server/handlers"
	"strconv"
	"time"
)

const (
	DefaultTcpPort   = 8081
	DefaultUdpPort   = 9091
	KeepAlivePeriod  = 30
)

func main() {
	// Определение флагов командной строки
	enableUdp := flag.Bool("udp", false, "Enable UDP server")
	udpPort := flag.Int("udpport", DefaultUdpPort, "UDP server port")

	// Парсинг флагов командной строки
	flag.Parse()

	// Получение TCP порта из оставшихся аргументов
	tcpPort := DefaultTcpPort
	args := flag.Args()
	if len(args) > 0 {
		portArg, err := strconv.Atoi(args[0])
		if err == nil && portArg > 0 && portArg < 65536 {
			tcpPort = portArg
		} else {
			fmt.Printf("Invalid TCP port specified: %s. Using default: %d\n", args[0], DefaultTcpPort)
		}
	} else {
		fmt.Printf("No TCP port specified. Using default: %d\n", DefaultTcpPort)
	}

	// Формирование адресов серверов
	tcpHostPort := fmt.Sprintf(":%d", tcpPort)
	udpHostPort := fmt.Sprintf(":%d", *udpPort)

	// Создание канала для ошибок
	errChan := make(chan error, 2)

	// Запуск UDP сервера при наличии флага
	if *enableUdp {
		go func() {
			fmt.Printf("Starting UDP server on port %d...\n", *udpPort)
			err := startUdpServer(udpHostPort)
			errChan <- err
		}()
	}

	// Запуск TCP сервера
	go func() {
		fmt.Printf("Starting TCP server on port %d...\n", tcpPort)
		err := startTcpServer(tcpHostPort)
		errChan <- err
	}()

	// Обработка ошибок
	for err := range errChan {
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}

func startTcpServer(hostPort string) error {
	ln, err := net.Listen("tcp", hostPort)
	if err != nil {
		return fmt.Errorf("Failed to listen: %v", err)
	}
	defer ln.Close()

	fmt.Printf("TCP server listening on %s\n", hostPort)

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

func startUdpServer(hostPort string) error {
	addr, err := net.ResolveUDPAddr("udp", hostPort)
	if err != nil {
		return fmt.Errorf("Failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("Failed to start UDP server: %v", err)
	}
	defer conn.Close()

	fmt.Printf("UDP server listening on %s\n", hostPort)

	handlers.HandleUdpConnections(conn)

	return nil
}
