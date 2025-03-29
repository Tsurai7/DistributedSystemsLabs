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
	DatagramSize  = 1400 // Recommended datagram size per ethernet mtu limitations
	SlidingWindow = 3    // We will receive ACK for 3 packages
	BuffSize      = 64 * 1024 * 1024
	UdpTimeout    = time.Millisecond * 1000
)

type Packet struct {
	SeqNum uint32
	Data   []byte
}

// ProgressBar displays a simple progress bar in console
func ProgressBar(current, total int, operation string) {
	const barLength = 50
	percent := float64(current) / float64(total)
	filled := int(barLength * percent)

	bar := "["
	for i := 0; i < barLength; i++ {
		if i < filled {
			bar += "="
		} else {
			bar += " "
		}
	}
	bar += "]"

	fmt.Printf("\r%s %s %.2f%% (%d/%d)", operation, bar, percent*100, current, total)
}

func HandleUdpConnections(conn *net.UDPConn) {
	buffer := make([]byte, DatagramSize)

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

	fmt.Printf("\nReceiving upload for file '%s' from %s\n", filename, addr.String())
	sendResponse(conn, addr, fmt.Sprintf("READY: Waiting for '%s' data", filename))

	conn.SetReadBuffer(BuffSize)

	// Проверяем, не существует ли файл
	if _, err := os.Stat(filename); err == nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: File '%s' already exists", filename))
		return
	}

	outputFile, err := os.Create(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Could not create file: %v", err))
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	buffer := make([]byte, DatagramSize)
	totalBytes := 0
	start := time.Now()
	noDataCount := 0
	lastProgressUpdate := time.Now()
	lastAckTime := time.Now()

	receivedChunks := make(map[int]bool)
	ackCounter := 0
	finalizing := false
	eofReceived := false

	finalTimeout := UdpTimeout * 5

	for {
		if time.Since(lastProgressUpdate) > 100*time.Millisecond {
			ProgressBar(totalBytes, totalBytes, "Receiving")
			lastProgressUpdate = time.Now()
		}

		currentTimeout := UdpTimeout
		if finalizing {
			currentTimeout = finalTimeout
		}
		conn.SetReadDeadline(time.Now().Add(currentTimeout))

		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if finalizing && time.Since(lastAckTime) > finalTimeout {
					fmt.Println("\nFinal timeout reached, completing upload")
					break
				}
				noDataCount++
				if noDataCount >= 3 {
					break
				}
				continue
			}
			fmt.Println("\nError receiving data:", err)
			return
		}

		if clientAddr.String() != addr.String() {
			continue
		}

		noDataCount = 0

		if n > 0 && string(buffer[:n]) == "EOF" {
			if !eofReceived {
				eofReceived = true
				finalizing = true
				fmt.Println("\nEOF marker received, finalizing upload...")
				conn.WriteToUDP([]byte("ACKEOF"), addr)
				continue
			}
		}

		if eofReceived {
			continue
		}

		if n > 0 && strings.HasPrefix(string(buffer[:n]), "RESEND:") {
			handleResendRequest(conn, addr, string(buffer[:n]), receivedChunks, DatagramSize)
			continue
		}

		chunkIndex := totalBytes / DatagramSize
		if !receivedChunks[chunkIndex] {
			if _, err := bufWriter.Write(buffer[:n]); err != nil {
				fmt.Println("\nError writing to file:", err)
				return
			}

			receivedChunks[chunkIndex] = true
			totalBytes += n
			ackCounter++
			lastAckTime = time.Now()

			if ackCounter >= SlidingWindow || chunkIndex == (totalBytes-1)/DatagramSize {
				ack := fmt.Sprintf("ACK:%d", chunkIndex)
				if _, err := conn.WriteToUDP([]byte(ack), addr); err != nil {
					fmt.Println("\nError sending ACK:", err)
				}
				ackCounter = 0
			}
		}
	}

	if err := bufWriter.Flush(); err != nil {
		fmt.Println("\nError flushing buffer:", err)
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Flush failed: %v", err))
		return
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes) / (1024 * 1024 * elapsed)
	ProgressBar(totalBytes, totalBytes, "Receiving")
	fmt.Printf("\nFile '%s' received successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes, elapsed, speed)

	finalResponse := fmt.Sprintf("SUCCESS: File '%s' uploaded (%d bytes)", filename, totalBytes)
	for i := 0; i < 3; i++ {
		if _, err := conn.WriteToUDP([]byte(finalResponse), addr); err != nil {
			fmt.Println("\nError sending final response:", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	conn.SetReadDeadline(time.Time{})
}

func handleResendRequest(conn *net.UDPConn, addr *net.UDPAddr, resendCmd string, receivedChunks map[int]bool, chunkSize int) {
	parts := strings.Split(resendCmd, ":")
	if len(parts) < 2 {
		fmt.Println("\nInvalid RESEND command:", resendCmd)
		return
	}

	chunkIndex, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("\nInvalid chunk index in RESEND command:", parts[1])
		return
	}

	if !receivedChunks[chunkIndex] {
		fmt.Printf("\nRequested chunk %d not found\n", chunkIndex)
		return
	}

	// Отправляем подтверждение для запрошенного чанка
	ack := fmt.Sprintf("ACK:%d", chunkIndex)
	conn.WriteToUDP([]byte(ack), addr)
	fmt.Printf("\nResent ACK for chunk %d\n", chunkIndex)
}

func sendResponse(conn *net.UDPConn, addr *net.UDPAddr, message string) {
	_, err := conn.WriteToUDP([]byte(message), addr)
	if err != nil {
		fmt.Println("\nError sending response:", err)
	}
}

func handleDownload(conn *net.UDPConn, addr *net.UDPAddr, filename string) {
	if filename == "" {
		sendResponse(conn, addr, "ERROR: Filename required for download")
		return
	}

	fileInfo, err := os.Stat(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: File not found: %v", err))
		return
	}

	if fileInfo.IsDir() {
		sendResponse(conn, addr, "ERROR: Cannot download directory")
		return
	}

	fileData, err := os.ReadFile(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Failed to read file: %v", err))
		return
	}

	fileSize := len(fileData)
	fmt.Printf("\nSending file '%s' (%d bytes) to %s\n", filename, fileSize, addr.String())

	// First send file size to client
	sendResponse(conn, addr, fmt.Sprintf("%d", fileSize))

	if fileSize <= DatagramSize {
		_, err = conn.WriteToUDP(fileData, addr)
		if err != nil {
			fmt.Printf("\nError sending file to client: %v\n", err)
		}
		return
	}

	conn.SetWriteBuffer(BuffSize)

	numChunks := (fileSize + DatagramSize - 1) / DatagramSize
	start := time.Now()
	sentBytes := 0
	lastProgressUpdate := time.Now()

	window := make([]Packet, SlidingWindow)
	ackChan := make(chan uint32, SlidingWindow)
	retryChan := make(chan uint32, SlidingWindow)

	go receiveACKs(conn, ackChan)

	for i := 0; i < numChunks; {
		if time.Since(lastProgressUpdate) > UdpTimeout {
			ProgressBar(sentBytes, fileSize, "Sending")
			lastProgressUpdate = time.Now()
		}

		for j := 0; j < SlidingWindow && i+j < numChunks; j++ {
			startPos := (i + j) * DatagramSize
			endPos := startPos + DatagramSize
			if endPos > fileSize {
				endPos = fileSize
			}

			packet := Packet{
				SeqNum: uint32(i + j),
				Data:   fileData[startPos:endPos],
			}

			window[j] = packet
			sendPacket(conn, addr, packet, retryChan)
		}

		for j := 0; j < SlidingWindow && i < numChunks; j++ {
			select {
			case ack := <-ackChan:
				if ack == uint32(i) {
					sentBytes += len(window[0].Data)
					i++
					window = window[1:]
					window = append(window, Packet{})
				}
			case seqNum := <-retryChan:
				fmt.Printf("\nRetrying packet %d\n", seqNum)
				for _, p := range window {
					if p.SeqNum == seqNum {
						sendPacket(conn, addr, p, retryChan)
						break
					}
				}
			case <-time.After(1 * time.Millisecond):
				// Timeout for non-acknowledged packets
			}
		}
	}

	_, err = conn.WriteToUDP([]byte("EOF"), addr)
	if err != nil {
		fmt.Println("\nError sending EOF marker:", err)
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(fileSize) / (1024 * 1024 * elapsed)
	ProgressBar(fileSize, fileSize, "Sending") // Complete progress bar
	fmt.Printf("\nFile '%s' sent successfully in %.2f seconds (%.2f MB/s)\n",
		filename, elapsed, speed)
}

func receiveACKs(conn *net.UDPConn, ackChan chan uint32) {
	buffer := make([]byte, 4)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("\nError receiving ACK: %v\n", err)
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

	_, err := conn.WriteToUDP(buf.Bytes(), addr)
	if err != nil {
		fmt.Printf("\nError sending packet %d: %v\n", packet.SeqNum, err)
		retryChan <- packet.SeqNum
	}
}
