package handlers

import (
	"io"
	"log"
	"net"
	"os"
	"time"
)

const (
	udpPort         = ":9091"
	packetSize      = 1280
	ackTimeout      = 2 * time.Second
	maxRetries      = 5
	commandEcho     = "echo"
	commandTime     = "time"
	commandUpload   = "upload"
	commandDownload = "download"
)

func HandleUdpConnections(conn *net.UDPConn) {
	buffer := make([]byte, packetSize)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Failed to read from UDP: %v", err)
			continue
		}

		command := string(buffer[:n])
		switch command {
		case commandEcho:
			handleEcho(conn, clientAddr)
		case commandTime:
			handleTime(conn, clientAddr)
		case commandUpload:
			handleUpload(conn, clientAddr)
		case commandDownload:
			handleDownload(conn, clientAddr)
		default:
			log.Printf("Unknown command: %s", command)
		}
	}
}

func handleEcho(conn *net.UDPConn, clientAddr *net.UDPAddr) {
	buffer := make([]byte, packetSize)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Failed to read echo data: %v", err)
			return
		}

		_, err = conn.WriteToUDP(buffer[:n], clientAddr)
		if err != nil {
			log.Printf("Failed to send echo response: %v", err)
			return
		}
	}
}

func handleTime(conn *net.UDPConn, clientAddr *net.UDPAddr) {
	currentTime := time.Now().Format(time.RFC3339)
	_, err := conn.WriteToUDP([]byte(currentTime), clientAddr)
	if err != nil {
		log.Printf("Failed to send time response: %v", err)
	}
}

// TODO: fix packaging 
func handleUpload(conn *net.UDPConn, clientAddr *net.UDPAddr) {
	fileName, err := receivePacket(conn, clientAddr)
	if err != nil {
		log.Printf("Failed to receive file name: %v", err)
		return
	}

	file, err := os.Create(string(fileName))
	if err != nil {
		log.Printf("Failed to create file: %v", err)
		return
	}
	defer file.Close()

	for {
		packet, err := receivePacket(conn, clientAddr)
		if err != nil {
			log.Printf("Failed to receive file data: %v", err)
			return
		}

		if len(packet) == 0 {
			break
		}

		_, err = file.Write(packet)
		if err != nil {
			log.Printf("Failed to write file data: %v", err)
			return
		}
	}

	log.Printf("File %s uploaded successfully", fileName)
}

// TODO: fix packaging 
func handleDownload(conn *net.UDPConn, clientAddr *net.UDPAddr) {
	fileName, err := receivePacket(conn, clientAddr)
	if err != nil {
		log.Printf("Failed to receive file name: %v", err)
		return
	}

	file, err := os.Open(string(fileName))
	if err != nil {
		log.Printf("Failed to open file: %v", err)
		sendPacket(conn, clientAddr, []byte("File not found"))
		return
	}
	defer file.Close()

	buffer := make([]byte, packetSize)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Failed to read file data: %v", err)
			return
		}

		err = sendPacket(conn, clientAddr, buffer[:n])
		if err != nil {
			log.Printf("Failed to send file data: %v", err)
			return
		}
	}

	err = sendPacket(conn, clientAddr, []byte{})
	if err != nil {
		log.Printf("Failed to send end-of-file marker: %v", err)
	}
}

func receivePacket(conn *net.UDPConn, clientAddr *net.UDPAddr) ([]byte, error) {
	buffer := make([]byte, packetSize)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, err
	}
	return buffer[:n], nil
}

func sendPacket(conn *net.UDPConn, clientAddr *net.UDPAddr, data []byte) error {
	_, err := conn.WriteToUDP(data, clientAddr)
	return err
}