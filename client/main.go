package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

const (
	serverAddr = "localhost:8081"
)

func main() {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	fmt.Printf("Connected to server at %s\n", serverAddr)

	reader := bufio.NewReader(os.Stdin)
	serverReader := bufio.NewReader(conn)

	for {
		fmt.Print("Enter command (time, echo, upload, download): ")
		cmd, _ := reader.ReadString('\n')
		cmd = strings.TrimSpace(cmd)

		switch cmd {
		case "time":
			sendCommand(conn, "time")
			response, _ := serverReader.ReadString('\n')
			fmt.Print("Server response: " + response)
		case "echo":
			sendCommand(conn, "echo")
			echoMode(conn, reader, serverReader)
		case "upload":
			sendCommand(conn, "upload")
			uploadFile(conn, reader, serverReader)
		case "download":
			sendCommand(conn, "download")
			downloadFile(conn, reader, serverReader)
		default:
			fmt.Println("Invalid command")
		}
	}
}

func sendCommand(conn net.Conn, cmd string) {
	_, err := conn.Write([]byte(cmd + "\n"))
	if err != nil {
		log.Fatalf("Failed to send command: %v", err)
	}
}

func echoMode(conn net.Conn, reader *bufio.Reader, serverReader *bufio.Reader) {
	for {
		fmt.Print("Enter message (type 'exit' to quit): ")
		msg, _ := reader.ReadString('\n')
		msg = strings.TrimSpace(msg)

		_, err := conn.Write([]byte(msg + "\n"))
		if err != nil {
			log.Fatalf("Failed to send message: %v", err)
		}

		if msg == "exit" {
			response, _ := serverReader.ReadString('\n')
			fmt.Print("Server response: " + response)
			return
		}

		response, _ := serverReader.ReadString('\n')
		fmt.Print("Server response: " + response)
	}
}

func uploadFile(conn net.Conn, reader *bufio.Reader, serverReader *bufio.Reader) {
	fmt.Print("Enter file name: ")
	fileName, _ := reader.ReadString('\n')
	fileName = strings.TrimSpace(fileName)

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return
	}
	defer file.Close()

	// Send file name to server
	_, err = conn.Write([]byte(fileName + "\n"))
	if err != nil {
		log.Fatalf("Failed to send file name: %v", err)
	}

	// Wait for server to acknowledge the file name
	response, _ := serverReader.ReadString('\n')
	fmt.Print("Server response: " + response)

	// Send file content to server
	_, err = io.Copy(conn, file)
	if err != nil {
		log.Fatalf("Failed to send file content: %v", err)
	}

	// Notify server that file upload is complete
	_, err = conn.Write([]byte("\n"))
	if err != nil {
		log.Fatalf("Failed to send end of file marker: %v", err)
	}

	response, _ = serverReader.ReadString('\n')
	fmt.Print("Server response: " + response)
}

func downloadFile(conn net.Conn, reader *bufio.Reader, serverReader *bufio.Reader) {
	fmt.Print("Enter file name: ")
	fileName, _ := reader.ReadString('\n')
	fileName = strings.TrimSpace(fileName)

	// Send file name to server
	_, err := conn.Write([]byte(fileName + "\n"))
	if err != nil {
		log.Fatalf("Failed to send file name: %v", err)
	}

	// Wait for server to acknowledge the file name
	response, _ := serverReader.ReadString('\n')
	fmt.Print("Server response: " + response)

	// Create a local file to save the downloaded content
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Printf("Failed to create file: %v\n", err)
		return
	}
	defer file.Close()

	// Copy server response to the file
	_, err = io.Copy(file, conn)
	if err != nil {
		fmt.Printf("Failed to save file content: %v\n", err)
		return
	}

	fmt.Println("File downloaded successfully")
}
