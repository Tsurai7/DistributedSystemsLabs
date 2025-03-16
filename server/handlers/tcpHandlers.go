package handlers

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
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
		cmdLine, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}
		cmdLine = strings.TrimSpace(cmdLine)

		parts := strings.Fields(cmdLine)
		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToUpper(parts[0])

		switch cmd {
		case "TIME":
			handleTimeCommand(writer)
		case "ECHO":
			param := ""
			if len(parts) > 1 {
				param = strings.Join(parts[1:], " ")
			}
			handleEchoCommand(reader, writer, param)
		case "UPLOAD":
			if len(parts) < 2 {
				sendTcpResponse(writer, "Upload failed: missing filename\n")
				continue
			}
			filename := parts[1]
			var fileSize int64 = -1
			if len(parts) > 2 {
				size, err := strconv.ParseInt(parts[2], 10, 64)
				if err == nil {
					fileSize = size
				}
			}
			handleUploadCommand(reader, writer, filename, fileSize)
		case "DOWNLOAD":
			if len(parts) < 2 {
				sendTcpResponse(writer, "Download failed: missing filename\n")
				continue
			}
			filename := parts[1]
			handleDownloadCommand(reader, writer, filename)
		default:
			sendTcpResponse(writer, fmt.Sprintf("Invalid command: %s\n", cmd))
		}
	}
}

func handleTimeCommand(writer *bufio.Writer) {
	currentTime := time.Now().Format(time.RFC3339)
	sendTcpResponse(writer, fmt.Sprintf("Current time: %s\n", currentTime))
}

func handleEchoCommand(reader *bufio.Reader, writer *bufio.Writer, initialMessage string) {
	if initialMessage != "" {
		sendTcpResponse(writer, fmt.Sprintf("%s\n", initialMessage))
		return
	}

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

func handleUploadCommand(reader *bufio.Reader, writer *bufio.Writer, filename string, fileSize int64) {
	file, err := os.Create(filename)
	if err != nil {
		sendTcpResponse(writer, fmt.Sprintf("Upload failed: could not create file %s: %v\n", filename, err))
		return
	}
	defer file.Close()

	sendTcpResponse(writer, "Ready to receive file. End with EOF\\n\n")

	bytesReceived := int64(0)
	buffer := make([]byte, 4096)
	eofMarkerFound := false

	for !eofMarkerFound {
		n, err := reader.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			sendTcpResponse(writer, fmt.Sprintf("Upload failed: error reading data: %v\n", err))
			return
		}

		data := buffer[:n]
		if n >= 4 {
			endStr := string(data[n-4 : n])
			if endStr == "EOF\n" {
				_, err = file.Write(data[:n-4])
				eofMarkerFound = true
				bytesReceived += int64(n - 4)
			} else {
				_, err = file.Write(data)
				bytesReceived += int64(n)
			}
		} else {
			_, err = file.Write(data)
			bytesReceived += int64(n)
		}

		if err != nil {
			sendTcpResponse(writer, fmt.Sprintf("Upload failed: error writing to file: %v\n", err))
			return
		}

		if fileSize > 0 && bytesReceived >= fileSize && eofMarkerFound {
			break
		}
	}

	sendTcpResponse(writer, fmt.Sprintf("File '%s' uploaded successfully. Received %d bytes.\n", filename, bytesReceived))
}

func handleDownloadCommand(reader *bufio.Reader, writer *bufio.Writer, filename string) {
	file, err := os.Open(filename)
	if err != nil {
		sendTcpResponse(writer, fmt.Sprintf("Download failed: could not open file %s: %v\n", filename, err))
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		sendTcpResponse(writer, fmt.Sprintf("Download failed: could not get file info: %v\n", err))
		return
	}

	sendTcpResponse(writer, fmt.Sprintf("Sending file: %s (%d bytes)\n", filename, fileInfo.Size()))

	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Download failed: error reading file: %v\n", err)
			return
		}

		_, err = writer.Write(buffer[:n])
		if err != nil {
			log.Printf("Download failed: error writing to connection: %v\n", err)
			return
		}
		writer.Flush()
	}

	writer.WriteString("EOF\n")
	writer.Flush()
}

func sendTcpResponse(writer *bufio.Writer, message string) {
	writer.WriteString(message)
	writer.Flush()
}
