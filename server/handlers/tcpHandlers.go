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

func HandleTcpConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		fmt.Printf("Connection closed from %s\n", conn.RemoteAddr())
	}()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		cmd, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}
		cmd = strings.TrimSpace(cmd)

		switch cmd {
		case "time":
			writer.WriteString(fmt.Sprintf("Current time: %v\n", time.Now().Format(time.RFC3339)))
		case "echo":
			writer.WriteString("Echo mode activated. Type 'exit' to quit.\n")
			echoMode(reader, writer)
		case "upload":
			writer.WriteString("Upload mode activated. Send file name and data.\n")
			uploadFile(reader, writer)
		case "download":
			writer.WriteString("Download mode activated. Send file name.\n")
			downloadFile(reader, writer)
		default:
			writer.WriteString("Invalid command\n")
		}

		writer.Flush()
	}
}

func echoMode(reader *bufio.Reader, writer *bufio.Writer) {
	for {
		msg, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		msg = strings.TrimSpace(msg)
		if msg == "exit" {
			writer.WriteString("Exiting echo mode\n")
			writer.Flush()
			return
		}
		writer.WriteString("Echo: " + msg + "\n")
		writer.Flush()
	}
}

func uploadFile(reader *bufio.Reader, writer *bufio.Writer) {
	fileName, err := reader.ReadString('\n')
	if err != nil {
		writer.WriteString("Upload failed: could not read file name\n")
		writer.Flush()
		return
	}
	fileName = strings.TrimSpace(fileName)

	// Create or truncate the file
	file, err := os.Create(fileName)
	if err != nil {
		writer.WriteString(fmt.Sprintf("Upload failed: could not create file %s\n", fileName))
		writer.Flush()
		return
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	if err != nil {
		writer.WriteString(fmt.Sprintf("Upload failed: could not write to file %s\n", fileName))
		writer.Flush()
		return
	}

	writer.WriteString("File uploaded successfully\n")
	writer.Flush()
}

func downloadFile(reader *bufio.Reader, writer *bufio.Writer) {
	// Read file name
	fileName, err := reader.ReadString('\n')
	if err != nil {
		writer.WriteString("Download failed: could not read file name\n")
		writer.Flush()
		return
	}
	fileName = strings.TrimSpace(fileName)

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		writer.WriteString(fmt.Sprintf("Download failed: could not open file %s\n", fileName))
		writer.Flush()
		return
	}
	defer file.Close()

	// Copy file data to the connection
	_, err = io.Copy(writer, file)
	if err != nil {
		writer.WriteString(fmt.Sprintf("Download failed: could not send file %s\n", fileName))
		writer.Flush()
		return
	}

	writer.Flush()
}
