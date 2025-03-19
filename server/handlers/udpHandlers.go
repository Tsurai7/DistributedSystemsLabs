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

// WORKS!
func handleUpload(conn *net.UDPConn, addr *net.UDPAddr, filename string) {
	if filename == "" {
		sendResponse(conn, addr, "ERROR: Filename required for upload")
		return
	}

	fmt.Printf("Receiving upload for file '%s' from %s\n", filename, addr.String())
	sendResponse(conn, addr, fmt.Sprintf("READY: Waiting for '%s' data", filename))

	// Увеличение буфера чтения
	conn.SetReadBuffer(1024 * 1024) // 1 МБ буфер

	// Создание файла для записи
	outputFile, err := os.Create(filename)
	if err != nil {
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Could not create file: %v", err))
		return
	}
	defer outputFile.Close()

	// Буферизованная запись в файл
	bufWriter := bufio.NewWriterSize(outputFile, 64*1024) // 64 КБ буфер
	defer bufWriter.Flush()

	buffer := make([]byte, 65507) // Максимальный размер UDP-пакета
	totalBytes := 0
	lastUpdate := time.Now()
	start := time.Now()
	noDataCount := 0

	// Битовая карта для отслеживания полученных чанков
	chunkSize := 1280
	receivedChunks := make(map[int]bool)

	for {
		// Установка таймаута чтения
		conn.SetReadDeadline(time.Now().Add(2 * time.Second)) // Таймаут 2 секунды

		// Очистка буфера перед чтением
		for i := range buffer {
			buffer[i] = 0
		}

		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				noDataCount++
				if noDataCount >= 3 { // 6 секунд без данных
					fmt.Println("No data for 6 seconds, assuming transfer complete")
					break
				}
				continue
			}
			fmt.Println("Error receiving data:", err)
			sendResponse(conn, addr, fmt.Sprintf("ERROR: Read failed: %v", err))
			return
		}

		// Игнорирование пакетов от других клиентов
		if clientAddr.String() != addr.String() {
			fmt.Println("Ignoring packet from unexpected client:", clientAddr.String())
			continue
		}

		noDataCount = 0 // Сброс счетчика отсутствия данных

		// Проверка маркера EOF
		if n > 0 && string(buffer[:n]) == "EOF" {
			fmt.Println("Received EOF marker, ending upload.")
			break
		}

		// Проверка запроса на доотправку
		if n > 0 && strings.HasPrefix(string(buffer[:n]), "RESEND:") {
			// Обработка запроса на доотправку
			handleResendRequest(conn, addr, string(buffer[:n]), receivedChunks, chunkSize)
			continue
		}

		// Запись данных в файл
		if _, err := bufWriter.Write(buffer[:n]); err != nil {
			fmt.Println("Error writing to file:", err)
			sendResponse(conn, addr, fmt.Sprintf("ERROR: Write failed: %v", err))
			return
		}

		// Обновление битовой карты полученных чанков
		chunkIndex := totalBytes / chunkSize
		receivedChunks[chunkIndex] = true

		totalBytes += n

		// Логирование прогресса
		if time.Since(lastUpdate) > 100*time.Millisecond {
			elapsed := time.Since(start).Seconds()
			if elapsed > 0 {
				speed := float64(totalBytes) / (1024 * 1024 * elapsed) // MB/s
				fmt.Printf("\rReceived: %d bytes (%.2f MB/s)", totalBytes, speed)
			}
			lastUpdate = time.Now()
		}
	}

	// Финализация записи
	if err := bufWriter.Flush(); err != nil {
		fmt.Println("Error flushing buffer:", err)
		sendResponse(conn, addr, fmt.Sprintf("ERROR: Flush failed: %v", err))
		return
	}

	// Вывод статистики
	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes) / (1024 * 1024 * elapsed) // MB/s
	fmt.Printf("\nFile '%s' received successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes, elapsed, speed)

	// Отправка финального подтверждения клиенту
	finalResponse := fmt.Sprintf("SUCCESS: File '%s' uploaded (%d bytes)", filename, totalBytes)
	_, err = conn.WriteToUDP([]byte(finalResponse), addr)
	if err != nil {
		fmt.Println("Error sending final response:", err)
	}

	conn.SetReadDeadline(time.Time{})
}

func handleResendRequest(conn *net.UDPConn, addr *net.UDPAddr, request string, receivedChunks map[int]bool, chunkSize int) {
	// Разбор запроса на доотправку
	parts := strings.Split(request, ":")
	if len(parts) < 2 {
		sendResponse(conn, addr, "ERROR: Invalid RESEND request")
		return
	}

	// Отправка запрошенных чанков
	for _, chunkIndex := range strings.Split(parts[1], ",") {
		index, err := strconv.Atoi(chunkIndex)
		if err != nil {
			fmt.Println("Invalid chunk index:", err)
			continue
		}

		if !receivedChunks[index] {
			sendResponse(conn, addr, fmt.Sprintf("RESEND:%d", index))
		}
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

func sendResponse(conn *net.UDPConn, addr *net.UDPAddr, message string) {
	_, err := conn.WriteToUDP([]byte(message), addr)
	if err != nil {
		fmt.Printf("Error sending response: %v\n", err)
	}
}
