package handlers

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	UdpDatagramSize = 1024
)

func HandleUdpConnections(conn *net.UDPConn) {
	buffer := make([]byte, UdpDatagramSize) // 1MB buffer for file transfers

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("Error reading from UDP: %v\n", err)
			continue
		}

		// Process the command in a goroutine to handle multiple clients
		processCommand(conn, addr, buffer[:n])
	}
}

// processCommand parses and routes the command to appropriate handler
func processCommand(conn *net.UDPConn, addr *net.UDPAddr, data []byte) {
	cmd := strings.TrimSpace(string(data))
	parts := strings.SplitN(cmd, " ", 2)

	if len(parts) == 0 {
		sendResponse(conn, addr, "ERROR: Empty command")
		return
	}

	command := strings.ToUpper(parts[0])
	var param string
	if len(parts) > 1 {
		param = parts[1]
	}

	switch command {
	case "ECHO":
		handleEcho(conn, addr, param)
	case "TIME":
		handleTime(conn, addr)
	case "UPLOAD":
		handleUpload(conn, addr, param)
	case "DOWNLOAD":
		handleDownload(conn, addr, param)
	default:
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Unknown command '%s'", command))
	}
}

func handleEcho(conn *net.UDPConn, addr *net.UDPAddr, text string) {
	sendResponse(conn, addr, text)
}

func handleTime(conn *net.UDPConn, addr *net.UDPAddr) {
	currentTime := time.Now().Format(time.RFC3339)
	sendResponse(conn, addr, currentTime)
}

func handleUpload(conn *net.UDPConn, addr *net.UDPAddr, filename string) {
	if filename == "" {
		sendResponse(conn, addr, "ERROR: Filename required for upload")
		return
	}

	fmt.Printf("Receiving upload for file '%s' from %s\n", filename, addr.String())
	sendResponse(conn, addr, fmt.Sprintf("READY: Waiting for '%s' data", filename))

	conn.SetReadBuffer(64 * 1024 * 1024) // 64 kbytes buff size

	outputFile, err := os.Create(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Could not create file: %v", err))
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, 64*1024*1024)
	defer bufWriter.Flush()

	chunkSize := 1500
	buffer := make([]byte, chunkSize)
	totalBytes := 0
	start := time.Now()
	noDataCount := 0

	receivedChunks := make(map[int]bool)
	ackCounter := 0

	fmt.Print("\033[H\033[2J") // Очистка экрана
	fmt.Println("Receiving upload for file:", filename)
	fmt.Println("----------------------------------------")

	for {
		conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				noDataCount++
				if noDataCount >= 3 {
					break
				}
				continue
			}
			fmt.Println("Error receiving data:", err)
			return
		}

		if clientAddr.String() != addr.String() {
			continue
		}

		noDataCount = 0

		if n > 0 && string(buffer[:n]) == "EOF" {
			break
		}

		if n > 0 && strings.HasPrefix(string(buffer[:n]), "RESEND:") {
			handleResendRequest(conn, addr, string(buffer[:n]), receivedChunks, chunkSize)
			continue
		}

		chunkIndex := totalBytes / chunkSize
		if !receivedChunks[chunkIndex] {
			if _, err := bufWriter.Write(buffer[:n]); err != nil {
				fmt.Println("Error writing to file:", err)
				return
			}

			receivedChunks[chunkIndex] = true
			totalBytes += n
			ackCounter++

			if ackCounter >= 3 {
				ack := fmt.Sprintf("ACK:%d", chunkIndex)
				conn.WriteToUDP([]byte(ack), addr)
				ackCounter = 0
			}
		}
	}

	if err := bufWriter.Flush(); err != nil {
		fmt.Println("Error flushing buffer:", err)
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Flush failed: %v", err))
		return
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' received successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes, elapsed, speed)

	finalResponse := fmt.Sprintf("SUCCESS: File '%s' uploaded (%d bytes)\n", filename, totalBytes)
	_, err = conn.WriteToUDP([]byte(finalResponse), addr)
	if err != nil {
		fmt.Println("Error sending final response:", err)
	}

	conn.SetReadDeadline(time.Time{})
}

func handleResendRequest(conn *net.UDPConn, addr *net.UDPAddr, resendCmd string, receivedChunks map[int]bool, chunkSize int) {
	parts := strings.Split(resendCmd, ":")
	if len(parts) < 2 {
		fmt.Println("Invalid RESEND command:", resendCmd)
		return
	}

	chunkIndex, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Invalid chunk index in RESEND command:", parts[1])
		return
	}

	if !receivedChunks[chunkIndex] {
		fmt.Printf("Requested chunk %d not found\n", chunkIndex)
		return
	}

	// Отправляем подтверждение для запрошенного чанка
	ack := fmt.Sprintf("ACK:%d", chunkIndex)
	conn.WriteToUDP([]byte(ack), addr)
	fmt.Printf("Resent ACK for chunk %d\n", chunkIndex)
}

func sendResponse(conn *net.UDPConn, addr *net.UDPAddr, message string) {
	_, err := conn.WriteToUDP([]byte(message), addr)
	if err != nil {
		fmt.Println("Error sending response:", err)
	}
}

func handleDownload(conn *net.UDPConn, addr *net.UDPAddr, filename string) {
	if filename == "" {
		sendResponse(conn, addr, "ERROR: Filename required for download")
		return
	}

	// Check if file exists
	fileData, err := os.ReadFile(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Failed to read file: %v", err))
		return
	}

	fileSize := len(fileData)
	fmt.Printf("Sending file '%s' (%d bytes) to %s\n", filename, fileSize, addr.String())

	// If file is smaller than chunk size, send it directly
	if fileSize <= 1280 {
		_, err = conn.WriteToUDP(fileData, addr)
		if err != nil {
			fmt.Printf("Error sending file to client: %v\n", err)
		}
		return
	}

	// For larger files, send in chunks
	chunkSize := 1280

	// Calculate number of chunks
	numChunks := (fileSize + chunkSize - 1) / chunkSize

	// Set larger buffer size for the UDP connection to improve throughput
	conn.SetWriteBuffer(1024 * 1024) // 1MB buffer

	// Send file in chunks - use goroutines for parallel sending
	start := time.Now()
	sentBytes := 0

	for i := 0; i < numChunks; i++ {
		// Calculate chunk boundaries
		startPos := i * chunkSize
		endPos := startPos + chunkSize
		if endPos > fileSize {
			endPos = fileSize
		}

		// Send the chunk
		_, err = conn.WriteToUDP(fileData[startPos:endPos], addr)
		if err != nil {
			fmt.Printf("Error sending chunk to client: %v\n", err)
			return
		}

		sentBytes += endPos - startPos

		// Print progress every 100 chunks or at the end
		if i%100 == 0 || i == numChunks-1 {
			elapsed := time.Since(start).Seconds()
			speed := float64(sentBytes) / (1024 * 1024 * elapsed) // MB/s
			fmt.Printf("\rProgress: %.1f%% (%d/%d bytes) - %.2f MB/s",
				float64(sentBytes)*100/float64(fileSize),
				sentBytes, fileSize, speed)
		}
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(fileSize) / (1024 * 1024 * elapsed) // MB/s
	fmt.Printf("\nFile '%s' sent successfully in %.2f seconds (%.2f MB/s)\n",
		filename, elapsed, speed)
}
