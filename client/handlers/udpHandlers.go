package handlers

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	UdpDatagramSize = 1400             // Recommended datagram size per ethernet mtu limitations
	SlidingWindow   = 3                // We will receive ACK for 3 packages
	BuffSize        = 64 * 1024 * 1024 // 64 MBs
	AckTimeout      = 5 * time.Millisecond
)

func HandleUDPCommands(conn *net.UDPConn, scanner *bufio.Scanner) {
	for {
		fmt.Println("\nUDP Commands:")
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
			sendUDPCommand(conn, "ECHO "+message)

		case "2", "TIME":
			sendUDPCommand(conn, "TIME")

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
			uploadFileUDP(conn, filename)

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
			downloadFileUDP(conn, filename)

		default:
			fmt.Println("Unknown command")
		}
	}
}

func sendUDPCommand(conn *net.UDPConn, command string) {
	_, err := conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending command:", err)
		return
	}

	response := make([]byte, UdpDatagramSize)
	n, _, err := conn.ReadFromUDP(response)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Printf("Response: %s\n", response[:n])
}

func uploadFileUDP(conn *net.UDPConn, filename string) {
	start := time.Now()

	fileData, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	fileSize := len(fileData)
	fmt.Printf("Starting upload of '%s' (%d bytes)\n", filename, fileSize)

	conn.SetWriteBuffer(BuffSize)

	uploadCmd := fmt.Sprintf("UPLOAD %s", filename)
	_, err = conn.Write([]byte(uploadCmd))
	if err != nil {
		fmt.Println("Error sending upload command:", err)
		return
	}

	respBuffer := make([]byte, BuffSize)
	conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	n, _, err := conn.ReadFromUDP(respBuffer)
	if err != nil {
		fmt.Println("Error receiving initial response:", err)
		return
	}

	initialResponse := string(respBuffer[:n])
	if !strings.HasPrefix(initialResponse, "READY:") {
		fmt.Println("Server not ready:", initialResponse)
		return
	}

	fmt.Println("Server response:", initialResponse)

	conn.SetReadDeadline(time.Time{})

	numChunks := (fileSize + UdpDatagramSize - 1) / UdpDatagramSize

	sentChunks := make([]bool, numChunks)
	ackedChunks := make([]bool, numChunks)
	nextChunk := 0

	fmt.Print("\033[H\033[2J") // Очистка экрана
	fmt.Println("Uploading file:", filename)
	fmt.Println("Total chunks:", numChunks)
	fmt.Println("Window size:", SlidingWindow)
	fmt.Println("----------------------------------------")

	for nextChunk < numChunks || !allAcked(ackedChunks) {
		for i := nextChunk; i < nextChunk+SlidingWindow && i < numChunks; i++ {
			if !sentChunks[i] {
				startPos := i * UdpDatagramSize
				endPos := startPos + UdpDatagramSize
				if endPos > fileSize {
					endPos = fileSize
				}

				_, err = conn.Write(fileData[startPos:endPos])
				if err != nil {
					fmt.Println("Error sending file chunk:", err)
					return
				}

				sentChunks[i] = true
			}
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(respBuffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				for i := nextChunk; i < nextChunk+SlidingWindow && i < numChunks; i++ {
					if !ackedChunks[i] {
						sentChunks[i] = false
					}
				}
				continue
			} else {
				fmt.Println("Error receiving ACK:", err)
				return
			}
		}

		ack := string(respBuffer[:n])
		if strings.HasPrefix(ack, "ACK:") {
			chunkIndex, err := strconv.Atoi(strings.TrimPrefix(ack, "ACK:"))
			if err != nil {
				fmt.Println("Invalid ACK received:", ack)
				return
			}

			for i := nextChunk; i <= chunkIndex && i < numChunks; i++ {
				ackedChunks[i] = true
			}

			if chunkIndex >= nextChunk {
				nextChunk = chunkIndex + 1
			}
		}
	}

	_, err = conn.Write([]byte("EOF"))
	if err != nil {
		fmt.Println("\nError sending EOF marker:", err)
	}

	retries := 3
	for i := 0; i < retries; i++ {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(respBuffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("\nRetry %d: No final response from server\n", i+1)
				continue
			} else {
				fmt.Println("\nError receiving final response:", err)
				break
			}
		} else {
			fmt.Println("\nServer response:", string(respBuffer[:n]))
			break
		}
	}

	conn.SetReadDeadline(time.Time{})

	elapsed := time.Since(start).Seconds()
	speed := float64(fileSize) / (1024 * 1024 * elapsed)
	fmt.Printf("File '%s' uploaded in %.2f seconds (%.2f MB/s)\n",
		filename, elapsed, speed)
}

func allAcked(ackedChunks []bool) bool {
	for _, acked := range ackedChunks {
		if !acked {
			return false
		}
	}
	return true
}

func downloadFileUDP(conn *net.UDPConn, filename string) {
	start := time.Now()
	conn.SetReadBuffer(BuffSize)

	_, err := conn.Write([]byte("DOWNLOAD " + filename))
	if err != nil {
		fmt.Println("Error sending download request:", err)
		return
	}

	outputFile, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating output file:", err)
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	buffer := make([]byte, UdpDatagramSize)
	totalBytes := 0
	noDataCount := 0
	expectedSeqNum := uint32(0)

	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))

		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				noDataCount++
				if noDataCount >= 30 {
					break
				}
				continue
			}
			fmt.Println("Error receiving data:", err)
			return
		}

		if n > 0 && string(buffer[:n]) == "EOF" {
			break
		}

		seqNum := binary.BigEndian.Uint32(buffer[:4])

		if seqNum == expectedSeqNum {
			_, err = bufWriter.Write(buffer[4:n])
			if err != nil {
				fmt.Println("Error writing to file:", err)
				return
			}

			totalBytes += n - 4
			expectedSeqNum++

			sendACK(conn, seqNum)
		} else if seqNum < expectedSeqNum {
			sendACK(conn, seqNum)
			fmt.Printf("Sent ACK for out-of-order packet %d\n", seqNum)
		}
	}

	conn.SetReadDeadline(time.Time{})
	bufWriter.Flush()

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' downloaded successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes, elapsed, speed)
}

func sendACK(conn *net.UDPConn, seqNum uint32) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, seqNum)
	_, err := conn.Write(buf)
	if err != nil {
		fmt.Printf("Error sending ACK for packet %d: %v\n", seqNum, err)
	}
}
