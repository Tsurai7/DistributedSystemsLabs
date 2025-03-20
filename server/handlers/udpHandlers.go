package handlers

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	UdpDatagramSize = 1400 // Recommended datagram size per ethernet mtu limitations
	SlidingWindow   = 3    // We will receive ACK for 3 packages
	BuffSize        = 64 * 1024 * 1024
	chunkSize       = 1400
	windowSize      = 10
	ackTimeout      = 100 * time.Millisecond
	maxRetries      = 3 // 64 MBs
)

const ()

type Packet struct {
	SeqNum uint32
	Data   []byte
}

func HandleUdpConnections(conn *net.UDPConn) {
	buffer := make([]byte, UdpDatagramSize)

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("Error reading from UDP: %v\n", err)
			continue
		}

		processCommand(conn, addr, buffer[:n])
	}
}

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

	conn.SetReadBuffer(BuffSize)

	outputFile, err := os.Create(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Could not create file: %v", err))
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	buffer := make([]byte, UdpDatagramSize)
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
			handleResendRequest(conn, addr, string(buffer[:n]), receivedChunks, UdpDatagramSize)
			continue
		}

		chunkIndex := totalBytes / UdpDatagramSize
		if !receivedChunks[chunkIndex] {
			if _, err := bufWriter.Write(buffer[:n]); err != nil {
				fmt.Println("Error writing to file:", err)
				return
			}

			receivedChunks[chunkIndex] = true
			totalBytes += n
			ackCounter++

			if ackCounter >= SlidingWindow {
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

	fileData, err := os.ReadFile(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Failed to read file: %v", err))
		return
	}

	fileSize := len(fileData)
	fmt.Printf("Sending file '%s' (%d bytes) to %s\n", filename, fileSize, addr.String())

	if fileSize <= UdpDatagramSize {
		_, err = conn.WriteToUDP(fileData, addr)
		if err != nil {
			fmt.Printf("Error sending file to client: %v\n", err)
		}
		return
	}

	conn.SetWriteBuffer(BuffSize)

	numChunks := (fileSize + chunkSize - 1) / chunkSize
	start := time.Now()
	sentBytes := 0

	window := make([]Packet, windowSize)
	ackChan := make(chan uint32, windowSize)
	retryChan := make(chan uint32, windowSize)

	go receiveACKs(conn, ackChan)

	for i := 0; i < numChunks; {
		for j := 0; j < windowSize && i+j < numChunks; j++ {
			startPos := (i + j) * chunkSize
			endPos := startPos + chunkSize
			if endPos > fileSize {
				endPos = fileSize
			}

			packet := Packet{
				SeqNum: uint32(i + j),
				Data:   fileData[startPos:endPos],
			}

			window[j] = packet
			sendPacket(conn, addr, packet, retryChan)
			fmt.Printf("Sent packet %d\n", packet.SeqNum)
		}

		for j := 0; j < windowSize && i < numChunks; j++ {
			select {
			case ack := <-ackChan:
				fmt.Printf("Received ACK for packet %d\n", ack)
				if ack == uint32(i) {
					i++
					sentBytes += len(window[0].Data)
					window = window[1:]
					window = append(window, Packet{})
				}
			case <-time.After(1 * time.Millisecond):
				fmt.Printf("Timeout for packet %d, retrying...\n", i)
				retryChan <- uint32(i)
			}
		}
	}

	_, err = conn.WriteToUDP([]byte("EOF"), addr)
	if err != nil {
		fmt.Println("Error sending EOF marker:", err)
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(fileSize) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' sent successfully in %.2f seconds (%.2f MB/s)\n",
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

func receiveACKs(conn *net.UDPConn, ackChan chan uint32) {
	buffer := make([]byte, 4)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("Error receiving ACK: %v\n", err)
			continue
		}

		if n == 4 {
			ack := binary.BigEndian.Uint32(buffer)
			ackChan <- ack
		}
	}
}

func sendPacket(conn *net.UDPConn, addr *net.UDPAddr, packet Packet, retryChan chan uint32) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet.SeqNum)
	buf.Write(packet.Data)

	_, err := conn.WriteToUDP(buf.Bytes(), addr) // Оставляем WriteToUDP, если используем ListenUDP
	if err != nil {
		fmt.Printf("Error sending packet %d: %v\n", packet.SeqNum, err)
		retryChan <- packet.SeqNum
	}
}
