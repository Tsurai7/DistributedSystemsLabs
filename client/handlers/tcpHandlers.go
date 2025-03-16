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

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Printf("Response: %s", response)
}

func uploadFileTCP(conn net.Conn, filename string) {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		fmt.Println("Error accessing file:", err)
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	command := fmt.Sprintf("UPLOAD %s %d\n", filename, fileInfo.Size())
	_, err = conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending upload command:", err)
		return
	}

	reader := bufio.NewReader(conn)

	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading server response:", err)
		return
	}
	fmt.Print(response)

	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading file:", err)
			return
		}

		_, err = conn.Write(buffer[:n])
		if err != nil {
			fmt.Println("Error sending file data:", err)
			return
		}
	}

	_, err = conn.Write([]byte("EOF\n"))
	if err != nil {
		fmt.Println("Error signaling end of file:", err)
		return
	}

	response, err = reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading completion response:", err)
		return
	}

	fmt.Print(response)
}

func downloadFileTCP(conn net.Conn, filename string) {
	command := fmt.Sprintf("DOWNLOAD %s\n", filename)
	_, err := conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending download command:", err)
		return
	}

	reader := bufio.NewReader(conn)

	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading server response:", err)
		return
	}

	if strings.HasPrefix(response, "Download failed") {
		fmt.Print(response)
		return
	}

	fmt.Print(response)

	outFile, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer outFile.Close()

	fileContent := make([]byte, 4096)
	eof := false

	for !eof {
		n, err := reader.Read(fileContent)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading from connection:", err)
			return
		}

		data := fileContent[:n]
		if n >= 4 && string(data[n-4:n]) == "EOF\n" {
			_, err = outFile.Write(data[:n-4])
			eof = true
		} else {
			_, err = outFile.Write(data)
		}

		if err != nil {
			fmt.Println("Error writing to file:", err)
			return
		}
	}

	fmt.Printf("File '%s' downloaded successfully\n", filename)
}
