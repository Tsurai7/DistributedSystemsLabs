package main

import (
	"bufio"
	"client/handlers"
	"fmt"
	"log"
	"net"
	"os"
)

const (
	tcpAddr = "127.0.0.1:8081"
	udpAddr = "127.0.0.1:9091"
)

func main() {
	tcpConn, err := net.Dial("tcp", tcpAddr)
	if err != nil {
		log.Fatal("Error connecting to TCP:", err)
		return
	}
	defer tcpConn.Close()
	log.Println("TCP connected to", tcpAddr)

	udpServerAddr, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		log.Fatal("Error resolving UDP address:", err)
		return
	}

	udpConn, err := net.DialUDP("udp", nil, udpServerAddr)
	if err != nil {
		log.Fatal("Error connecting to UDP:", err)
		return
	}
	defer udpConn.Close()
	log.Println("UDP connected to", udpAddr)

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Println("\nChoose protocol: 1 - TCP, 2 - UDP, 3 - Exit")
		if !scanner.Scan() {
			break
		}

		choice := scanner.Text()

		switch choice {
		case "1":
			handlers.HandleTCPCommands(tcpConn, scanner)
		case "2":
			handlers.HandleUDPCommands(udpConn, scanner)
		case "3":
			fmt.Println("Exiting...")
			return
		default:
			fmt.Println("Invalid choice, please try again")
		}
	}
}
