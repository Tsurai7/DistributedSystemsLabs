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
	DatagramSize  = 1500 // Recommended datagram size per ethernet mtu limitations
	SlidingWindow = 8    // We will receive ACK for 3 packages
	BuffSize      = 64 * 1024 * 1024
	UdpTimeout    = time.Millisecond * 100
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

	// Разбиваем команду на части, учитывая что и UPLOAD и DOWNLOAD могут иметь offset
	parts := strings.SplitN(cmd, " ", 3)

	if len(parts) == 0 {
		sendResponse(conn, addr, "ERROR: Empty command")
		return
	}

	command := strings.ToUpper(parts[0])

	switch command {
	case "ECHO":
		param := ""
		if len(parts) > 1 {
			param = parts[1]
		}
		handleEcho(conn, addr, param)

	case "TIME":
		handleTime(conn, addr)

	case "UPLOAD":
		// Обрабатываем два варианта:
		// 1. UPLOAD filename
		// 2. UPLOAD filename offset
		if len(parts) < 2 {
			sendResponse(conn, addr, "ERROR: Filename required for upload")
			return
		}

		params := []string{parts[1]} // filename
		if len(parts) > 2 {
			params = append(params, parts[2]) // добавляем offset если есть
		}
		handleUpload(conn, addr, params)

	case "DOWNLOAD":
		// Обрабатываем два варианта:
		// 1. DOWNLOAD filename
		// 2. DOWNLOAD filename offset
		if len(parts) < 2 {
			sendResponse(conn, addr, "ERROR: Filename required for download")
			return
		}

		params := []string{parts[1]} // filename
		if len(parts) > 2 {
			params = append(params, parts[2]) // добавляем offset если есть
		}
		handleDownload(conn, addr, params)

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

func handleUpload(conn *net.UDPConn, addr *net.UDPAddr, args []string) {
	defer conn.SetReadDeadline(time.Time{})
	if len(args) < 1 {
		sendResponse(conn, addr, "ERROR: Filename required for upload")
		return
	}

	filename := args[0]
	offset := 0
	if len(args) > 1 {
		var err error
		offset, err = strconv.Atoi(args[1])
		if err != nil || offset < 0 {
			sendResponse(conn, addr, "ERROR: Invalid offset value")
			return
		}
	}

	fmt.Printf("\nReceiving upload for file '%s' from %s (offset: %d)\n",
		filename, addr.String(), offset)

	// Открываем файл для дозаписи или создаем новый
	var outputFile *os.File
	var err error

	if offset > 0 {
		outputFile, err = os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
		if err == nil {
			_, err = outputFile.Seek(int64(offset), 0)
		}
	} else {
		outputFile, err = os.Create(filename)
	}

	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Could not open file: %v", err))
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	// Немедленная отправка подтверждения
	sendResponse(conn, addr, fmt.Sprintf("READY: Offset %d", offset))

	buffer := make([]byte, DatagramSize)
	totalBytes := offset
	start := time.Now()
	lastProgressUpdate := time.Now()
	lastAckTime := time.Now()
	eofReceived := false

	// Для отслеживания полученных чанков
	receivedChunks := make(map[int]bool)

	// Настройки таймаутов
	normalTimeout := UdpTimeout
	finalTimeout := UdpTimeout
	currentTimeout := normalTimeout

	for {
		// Обновляем прогресс
		if time.Since(lastProgressUpdate) > UdpTimeout {
			go ProgressBar(totalBytes, totalBytes, "Receiving")
			lastProgressUpdate = time.Now()
		}

		// Устанавливаем таймаут
		conn.SetReadDeadline(time.Now().Add(currentTimeout))

		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if eofReceived && time.Since(lastAckTime) > finalTimeout {
					break
				}
				continue
			}
			fmt.Println("\nError receiving data:", err)
			return
		}

		// Проверяем адрес отправителя
		if clientAddr.String() != addr.String() {
			continue
		}

		// Обработка EOF
		if n >= 3 && string(buffer[:3]) == "EOF" {
			if !eofReceived {
				eofReceived = true
				currentTimeout = finalTimeout
				fmt.Println("\nEOF marker received, finalizing...")
				sendResponse(conn, addr, "ACKEOF")
			}
			continue
		}

		// Обработка данных
		chunkIndex := totalBytes / DatagramSize
		if !receivedChunks[chunkIndex] {
			if _, err := bufWriter.Write(buffer[:n]); err != nil {
				fmt.Println("\nError writing to file:", err)
				sendResponse(conn, addr, fmt.Sprintf("ERROR: Write failed: %v", err))
				return
			}

			receivedChunks[chunkIndex] = true
			totalBytes += n
			lastAckTime = time.Now()

			// Отправляем подтверждение
			ack := fmt.Sprintf("ACK:%d", chunkIndex)
			if _, err := conn.WriteToUDP([]byte(ack), addr); err != nil {
				fmt.Println("\nError sending ACK:", err)
			}
		}
	}

	// Финальные операции
	if err := bufWriter.Flush(); err != nil {
		fmt.Println("\nError flushing buffer:", err)
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Flush failed: %v", err))
		return
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes-offset) / (1024 * 1024 * elapsed)
	go ProgressBar(totalBytes, totalBytes, "Receiving")
	fmt.Printf("\nFile '%s' received successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes-offset, elapsed, speed)

	// Отправляем финальное подтверждение
	finalResponse := fmt.Sprintf("SUCCESS: Received %d bytes (total %d)", totalBytes-offset, totalBytes)
	for i := 0; i < 3; i++ {
		sendResponse(conn, addr, finalResponse)
		time.Sleep(50 * time.Millisecond)
	}
}

//func sendResponse(conn *net.UDPConn, addr *net.UDPAddr, message string) {
//	if _, err := conn.WriteToUDP([]byte(message), addr); err != nil {
//		fmt.Println("Error sending response:", err)
//	}
//}

func sendResponseDownload(conn *net.UDPConn, addr *net.UDPAddr, message string) bool {
	if _, err := conn.WriteToUDP([]byte(message), addr); err != nil {
		fmt.Println("Error sending response:", err)
		return false
	}
	return true
}

func handleDownload(conn *net.UDPConn, addr *net.UDPAddr, args []string) {
	if len(args) < 1 {
		sendResponse(conn, addr, "ERROR: Filename required")
		return
	}
	defer conn.SetReadDeadline(time.Time{})

	filename := args[0]
	offset := 0
	if len(args) > 1 {
		var err error
		offset, err = strconv.Atoi(args[1])
		if err != nil || offset < 0 {
			sendResponse(conn, addr, "ERROR: Invalid offset")
			return
		}
	}

	fileInfo, err := os.Stat(filename)
	if err != nil {
		sendResponse(conn, addr, "FILE_NOT_FOUND")
		return
	}

	if fileInfo.IsDir() {
		sendResponse(conn, addr, "ERROR: Is directory")
		return
	}

	fileData, err := os.ReadFile(filename)
	if err != nil {
		sendResponse(conn, addr, "ERROR: Reading file")
		return
	}

	fileSize := len(fileData)
	if offset > fileSize {
		sendResponse(conn, addr, "ERROR: Offset too large")
		return
	}

	if fileSize == 0 {
		sendResponse(conn, addr, "ERROR: Empty file")
		return
	}

	fmt.Printf("\nSending '%s' (%d bytes) to %s from offset %d\n",
		filename, fileSize, addr, offset)

	// Send file size
	if !sendResponse(conn, addr, fmt.Sprintf("SIZE %d", fileSize)) {
		return
	}

	// Wait for ACK
	if !waitForACK(conn, addr) {
		fmt.Println("No ACK received")
		return
	}

	conn.SetWriteBuffer(BuffSize)
	remainingData := fileData[offset:]
	start := time.Now()

	// Sliding window implementation
	window := make([]Packet, SlidingWindow)
	ackChan := make(chan uint32, SlidingWindow)
	retryChan := make(chan uint32, SlidingWindow)

	go receiveACKs(conn, ackChan)

	startSeq := offset / DatagramSize
	i := 0
	numChunks := (len(remainingData) + DatagramSize - 1) / DatagramSize

	for i < numChunks {
		// Fill window
		for j := 0; j < SlidingWindow && i+j < numChunks; j++ {
			startPos := (i + j) * DatagramSize
			endPos := startPos + DatagramSize
			if endPos > len(remainingData) {
				endPos = len(remainingData)
			}

			packet := Packet{
				SeqNum: uint32(startSeq + i + j),
				Data:   remainingData[startPos:endPos],
			}
			window[j] = packet
			sendPacket(conn, addr, packet, retryChan)
		}

		// Process ACKs
		for j := 0; j < SlidingWindow && i < numChunks; j++ {
			select {
			case ack := <-ackChan:
				if ack >= uint32(startSeq+i) {
					acked := int(ack) - (startSeq + i) + 1
					i += acked
					window = window[acked:]
					window = append(window, make([]Packet, acked)...)
				}
			case seq := <-retryChan:
				for _, p := range window {
					if p.SeqNum == seq {
						sendPacket(conn, addr, p, retryChan)
						break
					}
				}
			case <-time.After(UdpTimeout):
				// Resend entire window on timeout
				for _, p := range window {
					if p.Data != nil {
						sendPacket(conn, addr, p, retryChan)
					}
				}
			}
		}
	}

	// Send EOF marker
	for i := 0; i < 3; i++ {
		sendResponse(conn, addr, "EOF")
		time.Sleep(50 * time.Millisecond)
	}

	elapsed := time.Since(start).Seconds()
	fmt.Printf("\nTransfer completed in %.2fs (%.2f MB/s)\n",
		elapsed, float64(len(remainingData))/(1024*1024*elapsed))
}

func sendResponse(conn *net.UDPConn, addr *net.UDPAddr, msg string) bool {
	_, err := conn.WriteToUDP([]byte(msg), addr)
	if err != nil {
		fmt.Println("Send error:", err)
		return false
	}
	return true
}

func waitForACK(conn *net.UDPConn, addr *net.UDPAddr) bool {
	buf := make([]byte, 3)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return false
	}
	return string(buf[:n]) == "ACK"
}

func receiveACKs(conn *net.UDPConn, ackChan chan<- uint32) {
	buf := make([]byte, 8)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		if n >= 4 {
			seq := binary.BigEndian.Uint32(buf[:4])
			ackChan <- seq
		} else if string(buf[:n]) == "ACK" {
			ackChan <- 0
		}
	}
}

func sendPacket(conn *net.UDPConn, addr *net.UDPAddr, p Packet, retryChan chan<- uint32) {
	buf := make([]byte, 4+len(p.Data))
	binary.BigEndian.PutUint32(buf[:4], p.SeqNum)
	copy(buf[4:], p.Data)

	_, err := conn.WriteToUDP(buf, addr)
	if err != nil {
		retryChan <- p.SeqNum
	}
}
