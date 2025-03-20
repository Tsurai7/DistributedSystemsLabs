package main

import (
	"fmt"
	"log"
	"net"
	"syscall"

	"github.com/smallnest/epoller"
)

const (
	TcpHostPort     = ":8081"
	UdpHostPort     = ":9091"
	KeepAlivePeriod = 30
	BufferSize      = 1024
)

func main() {
	// Создаем epoller
	poller, err := epoller.NewPoller(BufferSize)
	if err != nil {
		log.Fatalf("Failed to create poller: %v", err)
	}

	go startTcpServer(poller)
	go startUdpServer(poller)

	for {
		conns, err := poller.Wait(128) // Ожидаем события (максимум 128 за раз)
		if err != nil {
			log.Printf("Error in poller.Wait: %v", err)
			continue
		}

		for _, conn := range conns {
			handleConnection(conn)
		}
	}
}

func startTcpServer(poller *epoller.Epoll) {
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

		if err := setNonBlocking(conn); err != nil {
			log.Printf("Failed to set non-blocking mode: %v", err)
			conn.Close()
			continue
		}

		if err := (*poller).Add(conn); err != nil {
			log.Printf("Failed to add connection to poller: %v", err)
			conn.Close()
			continue
		}

		fmt.Printf("New TCP connection from %s\n", conn.RemoteAddr())
	}
}

func startUdpServer(poller *epoller.Epoll) {
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

	// Переводим UDP-сокет в неблокирующий режим
	if err := setNonBlocking(conn); err != nil {
		log.Fatalf("Failed to set non-blocking mode for UDP: %v", err)
	}

	// Добавляем UDP-сокет в epoller
	if err := (*poller).Add(conn); err != nil {
		log.Fatalf("Failed to add UDP connection to poller: %v", err)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, BufferSize)
	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Error reading from connection: %v", err)
		return
	}

	// Обработка данных
	data := buffer[:n]
	fmt.Printf("Received data from %s: %s\n", conn.RemoteAddr(), string(data))

	// Отправляем ответ
	response := fmt.Sprintf("Echo: %s", string(data))
	_, err = conn.Write([]byte(response))
	if err != nil {
		log.Printf("Error writing to connection: %v", err)
	}
}

// setNonBlocking переводит сокет в неблокирующий режим
func setNonBlocking(conn net.Conn) error {
	fd, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get syscall.Conn: %v", err)
	}

	var syscallErr error
	err = fd.Control(func(fd uintptr) {
		syscallErr = syscall.SetNonblock(int(fd), true)
	})
	if err != nil {
		return fmt.Errorf("failed to control fd: %v", err)
	}
	if syscallErr != nil {
		return fmt.Errorf("failed to set non-blocking mode: %v", syscallErr)
	}

	return nil
}
