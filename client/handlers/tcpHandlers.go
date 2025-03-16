package handlers

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

func HandleTCPCommands(conn net.Conn, scanner *bufio.Scanner) {
	for {
		fmt.Println("\nTCP Commands:")
		fmt.Println("1. ECHO <message>")
		fmt.Println("2. TIME")
		fmt.Println("3. UPLOAD <filename>")
		fmt.Println("4. DOWNLOAD <filename>")
		fmt.Println("5. Back to protocol selection")
		fmt.Print("Enter command: ")

		if !scanner.Scan() {
			return
		}
		cmd := scanner.Text()

		if cmd == "5" {
			return
		}

		parts := strings.SplitN(cmd, " ", 2)
		command := strings.ToUpper(parts[0])

		switch command {
		case "1", "ECHO":
			var message string
			if len(parts) > 1 {
				message = parts[1]
			} else {
				fmt.Print("Enter message to echo: ")
				if !scanner.Scan() {
					return
				}
				message = scanner.Text()
			}
			sendTCPCommand(conn, "ECHO "+message)

		case "2", "TIME":
			sendTCPCommand(conn, "TIME")

		case "3", "UPLOAD":
			var filename string
			if len(parts) > 1 {
				filename = parts[1]
			} else {
				fmt.Print("Enter filename to upload: ")
				if !scanner.Scan() {
					return
				}
				filename = scanner.Text()
			}
			uploadFileTCP(conn, filename)

		case "4", "DOWNLOAD":
			var filename string
			if len(parts) > 1 {
				filename = parts[1]
			} else {
				fmt.Print("Enter filename to download: ")
				if !scanner.Scan() {
					return
				}
				filename = scanner.Text()
			}
			downloadFileTCP(conn, filename)

		default:
			fmt.Println("Unknown command")
		}
	}
}

func sendTCPCommand(conn net.Conn, command string) {
	_, err := fmt.Fprintf(conn, command+"\n")
	if err != nil {
		fmt.Println("Error sending command:", err)
		return
	}

	// Read response from server
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Printf("Response: %s\n", response[:n])
}

func uploadFileTCP(conn net.Conn, filename string) {
	// Read file content
	fileData, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	// Send upload command with filename
	command := fmt.Sprintf("UPLOAD %s\n", filename)
	_, err = conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending upload command:", err)
		return
	}

	// Send file content
	_, err = conn.Write(fileData)
	if err != nil {
		fmt.Println("Error sending file:", err)
		return
	}

	// Signal end of file with a newline
	_, err = conn.Write([]byte("\n"))
	if err != nil {
		fmt.Println("Error sending end of file marker:", err)
		return
	}

	// Read response
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Printf("Response: %s\n", response[:n])
}

func downloadFileTCP(conn net.Conn, filename string) {
	// Send download command
	command := fmt.Sprintf("DOWNLOAD %s\n", filename)
	_, err := conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending download command:", err)
		return
	}

	// Create file to save the downloaded content
	outFile, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer outFile.Close()

	// Read file content from server
	buffer := make([]byte, 4096)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading file content:", err)
			return
		}

		// Check for end of file marker
		if n == 1 && buffer[0] == '\n' {
			break
		}

		// Write data to file
		_, err = outFile.Write(buffer[:n])
		if err != nil {
			fmt.Println("Error writing to file:", err)
			return
		}
	}

	fmt.Printf("File '%s' downloaded successfully\n", filename)
}
