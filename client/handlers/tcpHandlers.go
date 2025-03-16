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

// HandleTCPCommands обрабатывает команды по TCP
func HandleTCPCommands(conn net.Conn, scanner *bufio.Scanner) {
	// Создаем reader для получения ответов от сервера
	reader := bufio.NewReader(conn)

	// Проверяем, не получили ли мы редирект сразу при подключении
	log.Println("Checking for immediate redirect...")
	redirectedConn, err := checkForRedirect(conn, reader)
	if err != nil {
		log.Println("Error during redirection check:", err)
		return
	}

	// Если соединение было перенаправлено, используем новое
	if redirectedConn != nil {
		log.Println("Got redirected connection, switching...")
		conn = redirectedConn
		reader = bufio.NewReader(conn)
		log.Println("Successfully redirected to child server")
	}

	for {
		fmt.Println("\nTCP Commands:")
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
			sendTCPCommand(conn, reader, "ECHO "+message)

		case "2", "TIME":
			sendTCPCommand(conn, reader, "TIME")

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
			uploadFileTCP(conn, reader, filename)

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
			downloadFileTCP(conn, reader, filename)

		default:
			fmt.Println("Unknown command")
		}
	}
}

// Функция проверяет первый ответ от сервера на наличие редиректа
func checkForRedirect(conn net.Conn, reader *bufio.Reader) (net.Conn, error) {
	// Устанавливаем таймаут для чтения
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Пытаемся прочитать ответ от сервера
	log.Println("Waiting for potential redirect...")
	response, err := reader.ReadString('\n')

	// Сбрасываем таймаут
	conn.SetReadDeadline(time.Time{})

	// Если ошибка таймаута или соединение еще ничего не прислало, продолжаем с текущим соединением
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			log.Println("No redirect received (timeout)")
			return nil, nil // Возвращаем nil, что означает "нет редиректа"
		}
		return nil, fmt.Errorf("error reading response: %v", err)
	}

	log.Printf("Received response: %q\n", response)

	// Проверяем, содержит ли ответ команду редиректа
	if strings.HasPrefix(response, "REDIRECT") {
		parts := strings.Fields(response)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid redirect format: %s", response)
		}

		// Получаем новый порт
		port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid port in redirect: %s", parts[1])
		}

		// Используем 127.0.0.1 для локального соединения
		host := "127.0.0.1" // Всегда используем IP-адрес вместо hostname

		// Создаем новое соединение к дочернему серверу
		log.Printf("Redirecting to %s:%d...\n", host, port)

		// Закрываем текущее соединение перед созданием нового
		conn.Close()

		// Добавляем задержку перед подключением к новому порту
		time.Sleep(500 * time.Millisecond)

		newConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to redirected server: %v", err)
		}

		log.Printf("Connected to redirected server at %s:%d\n", host, port)

		// Читаем приветственное сообщение от дочернего сервера
		welcomeMsg, err := bufio.NewReader(newConn).ReadString('\n')
		if err != nil {
			log.Printf("Warning: Failed to read welcome message: %v", err)
			// Продолжаем работу даже при ошибке чтения приветствия
		} else {
			log.Printf("Received welcome message: %q", welcomeMsg)
		}

		return newConn, nil
	}

	log.Println("No redirect in response")
	return nil, nil
}

func sendTCPCommand(conn net.Conn, reader *bufio.Reader, command string) {
	log.Printf("Sending command: %q\n", command)
	_, err := fmt.Fprintf(conn, command+"\n")
	if err != nil {
		fmt.Println("Error sending command:", err)
		return
	}

	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	log.Printf("Received response: %q\n", response)

	// Проверяем, не является ли ответ редиректом
	if strings.HasPrefix(response, "REDIRECT") {
		newConn, err := handleRedirect(conn, response)
		if err != nil {
			fmt.Println("Error handling redirect:", err)
			return
		}

		// Повторяем команду на новом соединении
		sendTCPCommand(newConn, bufio.NewReader(newConn), command)
		return
	}

	fmt.Printf("Response: %s", response)
}

func uploadFileTCP(conn net.Conn, reader *bufio.Reader, filename string) {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		fmt.Println("Error accessing file:", err)
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	command := fmt.Sprintf("UPLOAD %s %d\n", filename, fileInfo.Size())
	log.Printf("Sending upload command: %q\n", command)
	_, err = conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending upload command:", err)
		return
	}

	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading server response:", err)
		return
	}

	log.Printf("Received response: %q\n", response)

	// Проверяем, не является ли ответ редиректом
	if strings.HasPrefix(response, "REDIRECT") {
		newConn, err := handleRedirect(conn, response)
		if err != nil {
			fmt.Println("Error handling redirect:", err)
			return
		}

		// Повторяем загрузку на новом соединении
		uploadFileTCP(newConn, bufio.NewReader(newConn), filename)
		return
	}

	fmt.Print(response)

	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading file:", err)
			return
		}

		_, err = conn.Write(buffer[:n])
		if err != nil {
			fmt.Println("Error sending file data:", err)
			return
		}
	}

	_, err = conn.Write([]byte("EOF\n"))
	if err != nil {
		fmt.Println("Error signaling end of file:", err)
		return
	}

	response, err = reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading completion response:", err)
		return
	}

	fmt.Print(response)
}

func downloadFileTCP(conn net.Conn, reader *bufio.Reader, filename string) {
	command := fmt.Sprintf("DOWNLOAD %s\n", filename)
	log.Printf("Sending download command: %q\n", command)
	_, err := conn.Write([]byte(command))
	if err != nil {
		fmt.Println("Error sending download command:", err)
		return
	}

	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Error reading server response:", err)
		return
	}

	log.Printf("Received response: %q\n", response)

	// Проверяем, не является ли ответ редиректом
	if strings.HasPrefix(response, "REDIRECT") {
		newConn, err := handleRedirect(conn, response)
		if err != nil {
			fmt.Println("Error handling redirect:", err)
			return
		}

		// Повторяем скачивание на новом соединении
		downloadFileTCP(newConn, bufio.NewReader(newConn), filename)
		return
	}

	if strings.HasPrefix(response, "Download failed") {
		fmt.Print(response)
		return
	}

	fmt.Print(response)

	outFile, err := os.Create(filename)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer outFile.Close()

	fileContent := make([]byte, 4096)
	eof := false

	for !eof {
		n, err := reader.Read(fileContent)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println("Error reading from connection:", err)
			return
		}

		data := fileContent[:n]
		if n >= 4 && string(data[n-4:n]) == "EOF\n" {
			_, err = outFile.Write(data[:n-4])
			eof = true
		} else {
			_, err = outFile.Write(data)
		}

		if err != nil {
			fmt.Println("Error writing to file:", err)
			return
		}
	}

	fmt.Printf("File '%s' downloaded successfully\n", filename)
}

// Функция для обработки редиректа
func handleRedirect(conn net.Conn, redirectMessage string) (net.Conn, error) {
	parts := strings.Fields(redirectMessage)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid redirect format: %s", redirectMessage)
	}

	// Получаем новый порт
	port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid port in redirect: %s", parts[1])
	}

	// Всегда используем 127.0.0.1 для локального подключения
	host := "127.0.0.1"

	// Создаем новое соединение к дочернему серверу
	log.Printf("Redirecting to %s:%d...\n", host, port)

	// Закрываем текущее соединение
	conn.Close()

	// Небольшая задержка перед новым подключением
	time.Sleep(500 * time.Millisecond)

	newConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redirected server: %v", err)
	}

	log.Printf("Connected to redirected server at %s:%d\n", host, port)

	// Читаем приветственное сообщение от дочернего сервера
	welcomeMsg, err := bufio.NewReader(newConn).ReadString('\n')
	if err != nil {
		log.Printf("Warning: Failed to read welcome message: %v", err)
		// Продолжаем работу даже при ошибке чтения приветствия
	} else {
		log.Printf("Received welcome message: %q", welcomeMsg)
	}

	return newConn, nil
}
