package handlers

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
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
	Timeout       = time.Millisecond * 100
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

func HandleUDPCommands(udpAddr string, scanner *bufio.Scanner) {
	udpServerAddr, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		log.Fatal("Error resolving UDP address:", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, udpServerAddr)
	if err != nil {
		log.Fatal("Error connecting to UDP:", err)
		return
	}
	defer conn.Close()
	log.Println("UDP connected to", udpAddr)

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
	// Закрываем соединение при выходе из функции
	defer conn.Close()

	start := time.Now()
	globalTimeout := 5 * time.Minute // Максимальное время выполнения всей операции
	lastActivity := time.Now()

	// Проверяем наличие частичной загрузки
	tempFilename := filename + ".part"
	var fileData []byte
	var existingSize int
	var err error

	if _, err := os.Stat(tempFilename); err == nil {
		fileData, err = os.ReadFile(filename)
		if err != nil {
			fmt.Println("Error reading original file:", err)
			return
		}

		partInfo, err := os.Stat(tempFilename)
		if err != nil {
			fmt.Println("Error reading partial upload:", err)
			return
		}
		existingSize = int(partInfo.Size())
		fmt.Printf("Resuming upload of '%s' from %d bytes\n", filename, existingSize)
	} else {
		fileData, err = os.ReadFile(filename)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return
		}
		existingSize = 0
	}

	fileSize := len(fileData)
	fmt.Printf("Uploading file '%s' (%d bytes total, %d bytes remaining)\n",
		filename, fileSize, fileSize-existingSize)

	conn.SetWriteBuffer(BuffSize)

	// Увеличиваем таймаут для начального ответа
	initialTimeout := Timeout
	conn.SetReadDeadline(time.Now().Add(initialTimeout))

	// Отправляем команду с offset
	uploadCmd := fmt.Sprintf("UPLOAD %s %d", filename, existingSize)
	_, err = conn.Write([]byte(uploadCmd))
	if err != nil {
		fmt.Println("Error sending upload command:", err)
		return
	}

	// Проверка глобального таймаута
	if time.Since(lastActivity) > globalTimeout {
		fmt.Println("\nGlobal timeout exceeded, closing connection")
		return
	}

	// Увеличиваем буфер для начального ответа
	respBuffer := make([]byte, BuffSize)
	n, _, err := conn.ReadFromUDP(respBuffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Println("\nServer not responding, retrying...")
			// Повторная попытка
			conn.SetReadDeadline(time.Now().Add(initialTimeout))
			_, err = conn.Write([]byte(uploadCmd))
			if err != nil {
				fmt.Println("Error resending upload command:", err)
				return
			}
			n, _, err = conn.ReadFromUDP(respBuffer)
			if err != nil {
				fmt.Println("Server still not responding after retry")
				return
			}
		} else {
			fmt.Println("Error receiving initial response:", err)
			return
		}
	}
	lastActivity = time.Now()

	initialResponse := string(respBuffer[:n])
	if !strings.HasPrefix(initialResponse, "READY:") {
		fmt.Println("Server not ready:", initialResponse)
		return
	}

	fmt.Println("Server response:", initialResponse)
	conn.SetReadDeadline(time.Time{}) // Сбрасываем таймаут

	numChunks := (fileSize + DatagramSize - 1) / DatagramSize
	sentChunks := make([]bool, numChunks)
	ackedChunks := make([]bool, numChunks)
	startChunk := existingSize / DatagramSize
	nextChunk := startChunk

	// Помечаем уже отправленные чанки
	for i := 0; i < startChunk; i++ {
		sentChunks[i] = true
		ackedChunks[i] = true
	}

	fmt.Println("\nUploading file:", filename)
	fmt.Printf("Total chunks: %d, Window size: %d, Starting from chunk: %d\n",
		numChunks, SlidingWindow, startChunk)

	for nextChunk < numChunks || !allAcked(ackedChunks) {
		// Проверка глобального таймаута
		if time.Since(lastActivity) > globalTimeout {
			fmt.Println("\nGlobal timeout exceeded, closing connection")
			savePartialUpload(tempFilename, fileData[:nextChunk*DatagramSize])
			return
		}

		ackedCount := countAcked(ackedChunks)
		ProgressBar(ackedCount*DatagramSize, fileSize, "Uploading")

		// Отправляем чанки в окне
		for i := nextChunk; i < nextChunk+SlidingWindow && i < numChunks; i++ {
			if !sentChunks[i] {
				startPos := i * DatagramSize
				endPos := startPos + DatagramSize
				if endPos > fileSize {
					endPos = fileSize
				}

				_, err = conn.Write(fileData[startPos:endPos])
				if err != nil {
					savePartialUpload(tempFilename, fileData[:i*DatagramSize])
					fmt.Println("\nError sending file chunk:", err)
					return
				}

				sentChunks[i] = true
				lastActivity = time.Now()
			}
		}

		conn.SetReadDeadline(time.Now().Add(Timeout))
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
				savePartialUpload(tempFilename, fileData[:nextChunk*DatagramSize])
				fmt.Println("\nConnection error:", err)
				return
			}
		}
		lastActivity = time.Now()

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

	os.Remove(tempFilename)

	_, err = conn.Write([]byte("EOF"))
	if err != nil {
		fmt.Println("\nError sending EOF marker:", err)
		return
	}
	lastActivity = time.Now()

	retries := 5
	for i := 0; i < retries; i++ {
		if time.Since(lastActivity) > globalTimeout {
			fmt.Println("\nGlobal timeout exceeded during final confirmation")
			return
		}

		conn.SetReadDeadline(time.Now().Add(Timeout))
		n, _, err := conn.ReadFromUDP(respBuffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("\nRetry %d: No final response from server\n", i+1)
				continue
			}
			fmt.Println("\nError receiving final response:", err)
			break
		}
		lastActivity = time.Now()
		fmt.Println("\nServer response:", string(respBuffer[:n]))
		break
	}

	conn.SetReadDeadline(time.Time{})

	elapsed := time.Since(start).Seconds()
	uploadedBytes := fileSize - existingSize
	speed := float64(uploadedBytes) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' uploaded in %.2f seconds (%.2f MB/s)\n",
		filename, elapsed, speed)
}

func savePartialUpload(filename string, data []byte) {
	err := os.WriteFile(filename, data, 0644)
	if err != nil {
		fmt.Println("Warning: failed to save upload progress:", err)
	}
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

	// Проверяем существование частично скачанного файла
	tempFilename := filename + ".part"
	fileInfo, err := os.Stat(tempFilename)
	var existingSize int64 = 0
	var outputFile *os.File

	if err == nil {
		// Файл существует, продолжаем докачку
		existingSize = fileInfo.Size()
		outputFile, err = os.OpenFile(tempFilename, os.O_APPEND|os.O_WRONLY, 0644)
		fmt.Printf("\nResuming download from %d bytes\n", existingSize)
	} else {
		// Новый файл
		outputFile, err = os.Create(tempFilename)
		fmt.Printf("\nStarting new download\n")
	}

	if err != nil {
		fmt.Println("Error opening output file:", err)
		return
	}
	defer outputFile.Close()

	// Отправляем запрос на скачивание с указанием текущей позиции
	downloadCmd := fmt.Sprintf("DOWNLOAD %s %d", filename, existingSize)
	_, err = conn.Write([]byte(downloadCmd))
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

	fmt.Printf("\nDownloading file '%s' (%d bytes total, %d bytes remaining)\n",
		filename, fileSize, fileSize-int(existingSize))

	bufWriter := bufio.NewWriterSize(outputFile, BuffSize)
	defer bufWriter.Flush()

	buffer := make([]byte, DatagramSize+4) // +4 для номера последовательности
	totalBytes := int(existingSize)
	expectedSeqNum := uint32(existingSize / int64(DatagramSize))
	lastProgressUpdate := time.Now()
	eofReceived := false

	pendingPackets := make(map[uint32][]byte)

	for totalBytes < fileSize && !eofReceived {
		if time.Since(lastProgressUpdate) > 100*time.Millisecond {
			ProgressBar(totalBytes, fileSize, "Downloading")
			lastProgressUpdate = time.Now()
		}

		conn.SetReadDeadline(time.Now().Add(Timeout))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("\nTimeout waiting for packet %d, resending ACK...", expectedSeqNum)
				sendACK(conn, expectedSeqNum-1)
				continue
			}
			fmt.Println("\nError receiving data:", err)
			return
		}

		if n >= 3 && string(buffer[:3]) == "EOF" {
			eofReceived = true
			fmt.Println("\nEOF marker received")
			continue
		}

		if n < 4 {
			continue
		}

		seqNum := binary.BigEndian.Uint32(buffer[:4])
		packetData := buffer[4:n]

		if seqNum == expectedSeqNum {
			if _, err := bufWriter.Write(packetData); err != nil {
				fmt.Println("\nError writing to file:", err)
				return
			}
			totalBytes += len(packetData)
			expectedSeqNum++

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

			sendACK(conn, seqNum)
		} else if seqNum > expectedSeqNum {
			if _, exists := pendingPackets[seqNum]; !exists {
				pendingPackets[seqNum] = packetData
			}
			sendACK(conn, expectedSeqNum-1)
		} else {
			sendACK(conn, seqNum)
		}
	}

	sendACK(conn, expectedSeqNum-1)

	if err := bufWriter.Flush(); err != nil {
		fmt.Println("\nError flushing buffer:", err)
		return
	}

	ProgressBar(fileSize, fileSize, "Downloading")

	// Переименовываем временный файл в окончательный
	if err := os.Rename(tempFilename, filename); err != nil {
		fmt.Println("\nError renaming temporary file:", err)
		return
	}

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes-int(existingSize)) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' downloaded successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes-int(existingSize), elapsed, speed)
}

func sendACK(conn *net.UDPConn, seqNum uint32) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, seqNum)
	_, err := conn.Write(buf)
	if err != nil {
		fmt.Printf("\nError sending ACK for packet %d: %v\n", seqNum, err)
	}
}
