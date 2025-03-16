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

	// Запускаем дочерний процесс сервера с флагом SkipTestConn,
	// чтобы он не завершался после проверочного соединения
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

	// Отправляем клиенту информацию о перенаправлении
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

	// Даем клиенту достаточно времени на переподключение
	time.Sleep(1 * time.Second)

	// Теперь закрываем проверочное соединение
	if testConn != nil {
		log.Printf("Closing test connection to child server on port %d", childPort)
		testConn.Close()
	}

	// Закрываем исходное соединение после того, как клиент переподключился
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

	// Ограничение - 2 соединения:
	// 1. Проверочное соединение от основного сервера
	// 2. Реальное соединение от клиента
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

		// Если это проверочное соединение, просто держим его открытым
		if connCount == 1 {
			// Обрабатываем проверочное соединение в отдельной горутине
			go func(c net.Conn) {
				defer c.Close()
				// Ждем закрытия соединения
				buffer := make([]byte, 1)
				c.Read(buffer) // Блокируется до закрытия соединения
				log.Printf("Test connection closed on port %d", port)
			}(conn)
		} else {
			// Это реальное клиентское соединение
			handleClientConnection(conn)
			// После отключения клиента завершаем работу
			log.Printf("Client disconnected from child server on port %d\n", port)
			return // Завершаем процесс после обработки клиентского соединения
		}
	}
}

func handleClientConnection(conn net.Conn) {
	defer conn.Close()

	// Сообщаем клиенту, что соединение установлено
	fmt.Fprintf(conn, "Hello from child server! You are connected.\n")

	// Улучшенная обработка клиентских сообщений с использованием bufio.Reader
	reader := bufio.NewReader(conn)
	for {
		// Чтение строки до символа новой строки
		message, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from client: %v", err)
			}
			break
		}

		// Обрабатываем полученное сообщение
		message = strings.TrimSpace(message)
		log.Printf("Received message: %s\n", message)

		// Простая обработка команд
		if strings.HasPrefix(message, "ECHO ") {
			response := strings.TrimPrefix(message, "ECHO ")
			fmt.Fprintf(conn, "%s\n", response)
		} else if message == "TIME" {
			fmt.Fprintf(conn, "%s\n", time.Now().Format(time.RFC3339))
		} else {
			// Эхо-ответ для неизвестных команд
			fmt.Fprintf(conn, "Echo: %s\n", message)
		}
	}
}
