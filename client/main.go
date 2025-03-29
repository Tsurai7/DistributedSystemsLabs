package main

import (
	"bufio"
	"client/handlers"
	"fmt"
	"os"
)

const (
	tcpAddr = "127.0.0.1:8081"
	udpAddr = "127.0.0.1:9091"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Println("\nChoose protocol: 1 - TCP, 2 - UDP, 3 - Exit")
		if !scanner.Scan() {
			break
		}

		choice := scanner.Text()
		switch choice {
		case "1":
			handlers.HandleTCPCommands(tcpAddr, scanner)
		case "2":
			handlers.HandleUDPCommands(udpAddr, scanner)
		case "3":
			fmt.Println("Exiting...")
			return
		default:
			fmt.Println("Invalid choice, please try again")
		}
	}
}
