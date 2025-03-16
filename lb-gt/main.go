package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MainServerPort  = 8081
	KeepAlivePeriod = 30 * time.Second
	StartupTimeout  = 5 * time.Second
)

// Информация о запущенном процессе-сервере
type childServer struct {
	cmd      *exec.Cmd
	port     int
	clientIP string
}

var (
	childServers = make(map[int]*childServer)
	nextPort     = MainServerPort + 1
	mu           sync.Mutex
)

func main() {
	// Настройка логгера для включения микросекунд
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Определяем, является ли это процесс дочерним сервером
	if len(os.Args) > 1 && os.Args[1] == "child" {
		if len(os.Args) < 3 {
			log.Fatalf("Child server requires port argument")
		}
		port, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("Invalid port for child server: %s", os.Args[2])
		}
		handleChildServer(port)
		return
	}

	// Основной сервер
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", MainServerPort))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	log.Printf("Main TCP server listening on :%d\n", MainServerPort)

	// Главный цикл принятия соединений
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		tcpConn := conn.(*net.TCPConn)
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(KeepAlivePeriod)

		clientIP := conn.RemoteAddr().String()
		log.Printf("New TCP connection from %s\n", clientIP)

		// Запускаем дочерний сервер и перенаправляем клиента
		go handleNewClient(tcpConn, clientIP)
	}
}

func handleNewClient(conn net.Conn, clientIP string) {
	// НЕ закрываем соединение сразу, чтобы дать время клиенту переподключиться
	// defer conn.Close()

	// Выбираем порт для дочернего сервера
	mu.Lock()
	childPort := nextPort
	nextPort++
	mu.Unlock()

	// Получаем полный путь к текущему исполняемому файлу
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path: %v", err)
		conn.Close()
		return
	}
	execPath, err = filepath.Abs(execPath)
	if err != nil {
		log.Printf("Failed to get absolute path: %v", err)
		conn.Close()
		return
	}

	// Запускаем дочерний процесс сервера
	cmd := exec.Command(execPath, "child", strconv.Itoa(childPort))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Запускаем дочерний процесс
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start child server: %v", err)
		conn.Close()
		return
	}

	// Регистрируем дочерний сервер
	child := &childServer{
		cmd:      cmd,
		port:     childPort,
		clientIP: clientIP,
	}

	mu.Lock()
	childServers[childPort] = child
	mu.Unlock()

	// Даем серверу время запуститься
	log.Printf("Waiting for child server on port %d to start...", childPort)
	time.Sleep(100 * time.Millisecond)

	// Проверяем доступность порта без закрытия проверочного соединения
	serverReady := false
	var testConn net.Conn

	for start := time.Now(); time.Since(start) < StartupTimeout; {
		testConn, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", childPort), 100*time.Millisecond)
		if err == nil {
			// Получили соединение, но НЕ закрываем его
			// Оставляем соединение открытым, чтобы дочерний сервер не завершил работу
			log.Printf("Successfully connected to 127.0.0.1:%d", childPort)
			serverReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !serverReady {
		log.Printf("Child server on port %d is not responding", childPort)
		cmd.Process.Kill()
		mu.Lock()
		delete(childServers, childPort)
		mu.Unlock()
		conn.Close()
		return
	}

	log.Printf("Redirecting client %s to child server on port %d\n", clientIP, childPort)
	_, err = fmt.Fprintf(conn, "REDIRECT %d\n", childPort)
	if err != nil {
		log.Printf("Failed to send redirect to client: %v", err)
		if testConn != nil {
			testConn.Close()
		}
		cmd.Process.Kill()
		mu.Lock()
		delete(childServers, childPort)
		mu.Unlock()
		conn.Close()
		return
	}

	// Даем клиенту время на переподключение
	time.Sleep(1 * time.Second)

	// Теперь можно закрыть тестовое соединение
	if testConn != nil {
		log.Printf("Closing test connection to child server on port %d", childPort)
		testConn.Close()
	}

	// Закрываем соединение с клиентом
	conn.Close()

	// Ждем завершения дочернего процесса
	go func() {
		err := cmd.Wait()
		mu.Lock()
		delete(childServers, childPort)
		mu.Unlock()
		if err != nil {
			log.Printf("Child server on port %d terminated with error: %v", childPort, err)
		} else {
			log.Printf("Child server on port %d terminated normally", childPort)
		}
	}()
}

func handleChildServer(port int) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Child server failed to listen on port %d: %v", port, err)
	}
	defer ln.Close()

	log.Printf("Child server listening on port %d\n", port)

	connCount := 0
	maxConn := 2

	for connCount < maxConn {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Child server accept error: %v", err)
			return
		}

		connCount++
		log.Printf("Child server on port %d accepted connection %d/%d from %s\n",
			port, connCount, maxConn, conn.RemoteAddr())

		if connCount == 1 {
			// Это тестовое соединение, держим его открытым
			go func(c net.Conn) {
				defer c.Close()
				buffer := make([]byte, 1)
				c.Read(buffer) // Блокируется до закрытия соединения
				log.Printf("Test connection closed on port %d", port)
			}(conn)
		} else {
			// Это клиентское соединение, обрабатываем его
			handleClientConnection(conn)
			log.Printf("Client disconnected from child server on port %d\n", port)
			return // Завершаем работу сервера после обработки клиента
		}
	}
}

func handleClientConnection(conn net.Conn) {
	defer conn.Close()

	// Отправляем приветственное сообщение
	fmt.Fprintf(conn, "Hello from child server! You are connected.\n")

	reader := bufio.NewReader(conn)
	for {
		// Читаем команду от клиента
		message, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from client: %v", err)
			}
			break
		}

		message = strings.TrimSpace(message)
		log.Printf("Received command: %s\n", message)

		// Парсим команду
		cmdParts := strings.Fields(message)
		if len(cmdParts) == 0 {
			continue
		}

		cmd := strings.ToUpper(cmdParts[0])

		switch {
		case cmd == "ECHO" && len(cmdParts) > 1:
			// Отправляем эхо-ответ
			response := strings.Join(cmdParts[1:], " ")
			fmt.Fprintf(conn, "%s\n", response)

		case cmd == "TIME":
			// Отправляем текущее время
			fmt.Fprintf(conn, "%s\n", time.Now().Format(time.RFC3339))

		case cmd == "UPLOAD" && len(cmdParts) >= 3:
			// Обрабатываем загрузку файла
			filename := cmdParts[1]
			fileSize, err := strconv.ParseInt(cmdParts[2], 10, 64)
			if err != nil {
				fmt.Fprintf(conn, "Upload failed: invalid file size\n")
				continue
			}
			handleFileUpload(conn, reader, filename, fileSize)

		case cmd == "DOWNLOAD" && len(cmdParts) >= 2:
			// Обрабатываем скачивание файла
			filename := cmdParts[1]
			handleFileDownload(conn, filename)

		default:
			// Неизвестная команда
			fmt.Fprintf(conn, "Echo: %s\n", message)
		}
	}
}

// Обработка загрузки файла от клиента
func handleFileUpload(conn net.Conn, reader *bufio.Reader, filename string, fileSize int64) {
	// Отправляем подтверждение готовности принять файл
	fmt.Fprintf(conn, "Ready to receive file '%s' (%d bytes)\n", filename, fileSize)

	// Создаем файл для записи данных
	outFile, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(conn, "Upload failed: %v\n", err)
		return
	}
	defer outFile.Close()

	// Читаем данные файла
	bytesReceived := int64(0)
	buffer := make([]byte, 4096)
	done := false

	for !done && bytesReceived < fileSize {
		n, err := reader.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(conn, "Upload failed: %v\n", err)
			return
		}

		data := buffer[:n]

		// Проверяем на маркер конца файла
		if n >= 4 && string(data[n-4:n]) == "EOF\n" {
			// Записываем данные без EOF
			_, err = outFile.Write(data[:n-4])
			bytesReceived += int64(n - 4)
			done = true
		} else {
			_, err = outFile.Write(data)
			bytesReceived += int64(n)
		}

		if err != nil {
			fmt.Fprintf(conn, "Upload failed: %v\n", err)
			return
		}
	}

	// Отправляем подтверждение успешной загрузки
	fmt.Fprintf(conn, "File '%s' uploaded successfully (%d bytes)\n", filename, bytesReceived)
}

// Обработка скачивания файла клиентом
func handleFileDownload(conn net.Conn, filename string) {
	// Проверяем существование файла
	fileInfo, err := os.Stat(filename)
	if err != nil {
		fmt.Fprintf(conn, "Download failed: %v\n", err)
		return
	}

	// Открываем файл для чтения
	file, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(conn, "Download failed: %v\n", err)
		return
	}
	defer file.Close()

	// Отправляем информацию о файле
	fmt.Fprintf(conn, "Sending file '%s' (%d bytes)\n", filename, fileInfo.Size())

	// Отправляем содержимое файла
	buffer := make([]byte, 4096)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Error reading file: %v", err)
			return
		}

		_, err = conn.Write(buffer[:n])
		if err != nil {
			log.Printf("Error sending file data: %v", err)
			return
		}
	}

	// Отправляем маркер конца файла
	_, err = conn.Write([]byte("EOF\n"))
	if err != nil {
		log.Printf("Error signaling end of file: %v", err)
		return
	}
}
