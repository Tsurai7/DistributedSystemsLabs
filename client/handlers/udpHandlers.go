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
	DatagramSize  = 1400             // Recommended datagram size per ethernet mtu limitations (1500 bytes)
	SlidingWindow = 3                // We will receive ACK for 3 packages
	BuffSize      = 64 * 1024 * 1024 // 64 MBs
	Timeout       = time.Millisecond * 1000
)

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

	response := make([]byte, DatagramSize)
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
	conn.SetReadDeadline(time.Now().Add(Timeout))
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

	numChunks := (fileSize + DatagramSize - 1) / DatagramSize
	sentChunks := make([]bool, numChunks)
	ackedChunks := make([]bool, numChunks)
	nextChunk := 0

	fmt.Println("\nUploading file:", filename)
	fmt.Printf("Total chunks: %d, Window size: %d\n", numChunks, SlidingWindow)

	for nextChunk < numChunks || !allAcked(ackedChunks) {
		ackedCount := countAcked(ackedChunks)
		ProgressBar(ackedCount, numChunks, "Uploading")

		for i := nextChunk; i < nextChunk+SlidingWindow && i < numChunks; i++ {
			if !sentChunks[i] {
				startPos := i * DatagramSize
				endPos := startPos + DatagramSize
				if endPos > fileSize {
					endPos = fileSize
				}

				_, err = conn.Write(fileData[startPos:endPos])
				if err != nil {
					fmt.Println("\nError sending file chunk:", err)
					return
				}

				sentChunks[i] = true
			}
		}

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
				fmt.Println("\nError receiving ACK:", err)
				return
			}
		}

		ack := string(respBuffer[:n])
		if strings.HasPrefix(ack, "ACK:") {
			chunkIndex, err := strconv.Atoi(strings.TrimPrefix(ack, "ACK:"))
			if err != nil {
				fmt.Println("\nInvalid ACK received:", ack)
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
		return
	}

	retries := 5
	for i := 0; i < retries; i++ {
		conn.SetReadDeadline(time.Now().Add(Timeout))
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
	fmt.Printf("\nFile '%s' uploaded in %.2f seconds (%.2f MB/s)\n",
		filename, elapsed, speed)
}

func countAcked(ackedChunks []bool) int {
	count := 0
	for _, acked := range ackedChunks {
		if acked {
			count++
		}
	}
	return count
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

	// Отправляем запрос на скачивание
	_, err := conn.Write([]byte("DOWNLOAD " + filename))
	if err != nil {
		fmt.Println("Error sending download request:", err)
		return
	}

	// Получаем размер файла от сервера
	fileSizeBuffer := make([]byte, DatagramSize)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := conn.ReadFromUDP(fileSizeBuffer)
	if err != nil {
		fmt.Println("Error receiving file size:", err)
		return
	}

	if string(fileSizeBuffer[:n]) == "FILE_NOT_FOUND" {
		fmt.Println("Error: File not found on server")
		return
	}

	fileSize, err := strconv.Atoi(string(fileSizeBuffer[:n]))
	if err != nil {
		fmt.Println("Error parsing file size:", err)
		return
	}

	fmt.Printf("\nDownloading file '%s' (%d bytes)\n", filename, fileSize)

	// Создаем файл для записи
	outputFile, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating output file:", err)
		return
	}
	defer outputFile.Close()

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	buffer := make([]byte, DatagramSize+4) // +4 для номера последовательности
	totalBytes := 0
	expectedSeqNum := uint32(0)
	lastProgressUpdate := time.Now()
	eofReceived := false

	// Карта для хранения полученных, но еще не записанных пакетов
	pendingPackets := make(map[uint32][]byte)

	for totalBytes < fileSize && !eofReceived {
		// Обновляем прогресс каждые 100мс
		if time.Since(lastProgressUpdate) > 100*time.Millisecond {
			ProgressBar(totalBytes, fileSize, "Downloading")
			lastProgressUpdate = time.Now()
		}

		conn.SetReadDeadline(time.Now().Add(Timeout))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Таймаут - повторяем запрос последнего ожидаемого пакета
				fmt.Printf("\nTimeout waiting for packet %d, resending ACK...", expectedSeqNum)
				sendACK(conn, expectedSeqNum-1) // Подтверждаем последний успешный пакет
				continue
			}
			fmt.Println("\nError receiving data:", err)
			return
		}

		// Проверяем маркер EOF
		if n >= 3 && string(buffer[:3]) == "EOF" {
			eofReceived = true
			fmt.Println("\nEOF marker received")
			continue
		}

		// Минимальный размер пакета - 4 байта (номер последовательности)
		if n < 4 {
			continue
		}

		seqNum := binary.BigEndian.Uint32(buffer[:4])
		packetData := buffer[4:n]

		// Если получили ожидаемый пакет
		if seqNum == expectedSeqNum {
			// Записываем данные в файл
			if _, err := bufWriter.Write(packetData); err != nil {
				fmt.Println("\nError writing to file:", err)
				return
			}
			totalBytes += len(packetData)
			expectedSeqNum++

			// Проверяем, есть ли следующие пакеты в pending
			for {
				if nextData, ok := pendingPackets[expectedSeqNum]; ok {
					if _, err := bufWriter.Write(nextData); err != nil {
						fmt.Println("\nError writing pending data to file:", err)
						return
					}
					totalBytes += len(nextData)
					delete(pendingPackets, expectedSeqNum)
					expectedSeqNum++
				} else {
					break
				}
			}

			// Отправляем подтверждение
			sendACK(conn, seqNum)
		} else if seqNum > expectedSeqNum {
			// Получили пакет из будущего - сохраняем во временное хранилище
			if _, exists := pendingPackets[seqNum]; !exists {
				pendingPackets[seqNum] = packetData
			}
			// Подтверждаем последний успешно полученный пакет
			sendACK(conn, expectedSeqNum-1)
		} else {
			// Получили старый пакет - просто подтверждаем
			sendACK(conn, seqNum)
		}
	}

	// Финальное подтверждение получения всех данных
	sendACK(conn, expectedSeqNum-1)

	// Убедимся, что все данные записаны
	if err := bufWriter.Flush(); err != nil {
		fmt.Println("\nError flushing buffer:", err)
		return
	}

	ProgressBar(fileSize, fileSize, "Downloading")

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
		fmt.Printf("\nError sending ACK for packet %d: %v\n", seqNum, err)
	}
}
