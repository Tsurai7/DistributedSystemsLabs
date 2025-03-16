package handlers

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

func HandleTcpConnections(conn net.Conn) {
	defer func() {
		conn.Close()
		fmt.Printf("Connection closed from %s\n", conn.RemoteAddr())
	}()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		// Read the incoming command
		cmdLine, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}
		cmdLine = strings.TrimSpace(cmdLine)

		// Parse the command
		parts := strings.SplitN(cmdLine, " ", 2)
		cmd := strings.ToUpper(parts[0])
		var param string
		if len(parts) > 1 {
			param = parts[1]
		}

		switch cmd {
		case "TIME":
			handleTimeCommand(writer)
		case "ECHO":
			handleEchoCommand(reader, writer, param)
		case "UPLOAD":
			handleUploadCommand(reader, writer, param)
		case "DOWNLOAD":
			handleDownloadCommand(reader, writer, param)
		default:
			sendTcpResponse(writer, fmt.Sprintf("Invalid command: %s\n", cmd))
		}
	}
}

// handleTimeCommand returns the current server time
func handleTimeCommand(writer *bufio.Writer) {
	currentTime := time.Now().Format(time.RFC3339)
	sendTcpResponse(writer, fmt.Sprintf("Current time: %s\n", currentTime))
}

// handleEchoCommand processes the echo command
func handleEchoCommand(reader *bufio.Reader, writer *bufio.Writer, initialMessage string) {
	// If there's an initial message with the command, echo it back
	if initialMessage != "" {
		sendTcpResponse(writer, fmt.Sprintf("%s\n", initialMessage))
		return
	}

	// Otherwise enter interactive echo mode
	sendTcpResponse(writer, "Echo mode activated. Type 'exit' to quit.\n")

	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		msg = strings.TrimSpace(msg)
		if msg == "exit" {
			sendTcpResponse(writer, "Exiting echo mode\n")
			return
		}
		sendTcpResponse(writer, fmt.Sprintf("Echo: %s\n", msg))
	}
}

// handleUploadCommand processes a file upload request
func handleUploadCommand(reader *bufio.Reader, writer *bufio.Writer, filename string) {
	// Handle filename from command or next line
	if filename == "" {
		// Read filename from the next line
		var err error
		filename, err = reader.ReadString('\n')
		if err != nil {
			sendTcpResponse(writer, "Upload failed: could not read filename\n")
			return
		}
		filename = strings.TrimSpace(filename)
	}

	// Create or truncate the file
	file, err := os.Create(filename)
	if err != nil {
		sendTcpResponse(writer, fmt.Sprintf("Upload failed: could not create file %s: %v\n", filename, err))
		return
	}
	defer file.Close()

	// Read the file data
	sendTcpResponse(writer, "Ready to receive file data. End with a newline.\n")

	// Buffer to read file content
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			sendTcpResponse(writer, fmt.Sprintf("Upload failed: error reading data: %v\n", err))
			return
		}

		// Check for end marker (newline)
		if n == 1 && buffer[0] == '\n' {
			break
		}

		// Write to file
		_, err = file.Write(buffer[:n])
		if err != nil {
			sendTcpResponse(writer, fmt.Sprintf("Upload failed: error writing to file: %v\n", err))
			return
		}
	}

	sendTcpResponse(writer, fmt.Sprintf("File '%s' uploaded successfully\n", filename))
}

// handleDownloadCommand processes a file download request
func handleDownloadCommand(reader *bufio.Reader, writer *bufio.Writer, filename string) {
	// Handle filename from command or next line
	if filename == "" {
		// Read filename from the next line
		var err error
		filename, err = reader.ReadString('\n')
		if err != nil {
			sendTcpResponse(writer, "Download failed: could not read filename\n")
			return
		}
		filename = strings.TrimSpace(filename)
	}

	// Open the file
	file, err := os.Open(filename)
	if err != nil {
		sendTcpResponse(writer, fmt.Sprintf("Download failed: could not open file %s: %v\n", filename, err))
		return
	}
	defer file.Close()

	// Send file content
	sendTcpResponse(writer, fmt.Sprintf("Sending file: %s\n", filename))

	// Buffer for reading from file
	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			sendTcpResponse(writer, fmt.Sprintf("Download failed: error reading file: %v\n", err))
			return
		}

		// Write to connection
		_, err = writer.Write(buffer[:n])
		if err != nil {
			log.Printf("Download failed: error writing to connection: %v\n", err)
			return
		}
		writer.Flush()
	}

	// Signal end of file with a newline
	writer.WriteByte('\n')
	writer.Flush()
}

// sendResponse is a helper function to send a response and flush the writer
func sendTcpResponse(writer *bufio.Writer, message string) {
	writer.WriteString(message)
	writer.Flush()
}
