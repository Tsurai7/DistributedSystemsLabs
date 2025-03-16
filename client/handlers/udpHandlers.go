package handlers

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	UdpDatagramSize = 1024
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

	// Read response from server
	response := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(response)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}

	fmt.Printf("Response: %s\n", response[:n])
}

func uploadFileUDP(conn *net.UDPConn, filename string) {
    start := time.Now()
    
    // Read the file to be uploaded
    fileData, err := os.ReadFile(filename)
    if err != nil {
        fmt.Println("Error reading file:", err)
        return
    }
    
    fileSize := len(fileData)
    fmt.Printf("Starting upload of '%s' (%d bytes)\n", filename, fileSize)
    
    // Increase UDP send buffer size
    conn.SetWriteBuffer(8 * 1024 * 1024) // 8MB buffer
    
    // Send upload command with filename only
    uploadCmd := fmt.Sprintf("UPLOAD %s", filename)
    _, err = conn.Write([]byte(uploadCmd))
    if err != nil {
        fmt.Println("Error sending upload command:", err)
        return
    }
    
    // Wait for READY response from server
    respBuffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
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
    
    // Reset deadline
    conn.SetReadDeadline(time.Time{})
    
    // A slight delay to ensure server is in upload mode
    time.Sleep(100 * time.Millisecond)
    
    // Define chunk size
    chunkSize := 1280
    
    // Calculate number of chunks
    numChunks := (fileSize + chunkSize - 1) / chunkSize
    
    // Send file in chunks
    sentBytes := 0
    lastUpdate := time.Now()
    
    for i := 0; i < numChunks; i++ {
        // Calculate chunk boundaries
        startPos := i * chunkSize
        endPos := startPos + chunkSize
        if endPos > fileSize {
            endPos = fileSize
        }
        
        // Send the chunk
        _, err = conn.Write(fileData[startPos:endPos])
        if err != nil {
            fmt.Println("Error sending file chunk:", err)
            return
        }
        
        sentBytes += endPos - startPos
        
        // Update progress at most 10 times per second
        if time.Since(lastUpdate) > 100*time.Millisecond || i == numChunks-1 {
            elapsed := time.Since(start).Seconds()
            speed := float64(sentBytes) / (1024 * 1024 * elapsed) // MB/s
            fmt.Printf("\rProgress: %.1f%% (%d/%d bytes) - %.2f MB/s", 
                      float64(sentBytes)*100/float64(fileSize), 
                      sentBytes, fileSize, speed)
            lastUpdate = time.Now()
        }
    }
    
    // Wait a moment to ensure all chunks are processed
    time.Sleep(100 * time.Millisecond)
    
    // Signal end of file by sending a small termination packet
    _, err = conn.Write([]byte("EOF"))
    if err != nil {
        fmt.Println("\nError sending EOF marker:", err)
    }
    
    // Wait for final acknowledgment from server
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    n, _, err = conn.ReadFromUDP(respBuffer)
    if err != nil {
        if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
            fmt.Println("\nNo final response from server, but upload may have succeeded")
        } else {
            fmt.Println("\nError receiving final response:", err)
        }
    } else {
        fmt.Println("\nServer response:", string(respBuffer[:n]))
    }
    
    elapsed := time.Since(start).Seconds()
    speed := float64(fileSize) / (1024 * 1024 * elapsed) // MB/s
    fmt.Printf("File '%s' uploaded in %.2f seconds (%.2f MB/s)\n", 
              filename, elapsed, speed)
}



func downloadFileUDP(conn *net.UDPConn, filename string) {
	start := time.Now()
	conn.SetReadBuffer(8 * 1024 * 1024)

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

	bufWriter := bufio.NewWriterSize(outputFile, 64*1024)
	defer bufWriter.Flush()

	buffer := make([]byte, 1500)
	totalBytes := 0
	lastUpdate := time.Now()
	noDataCount := 0

	for {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

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

		noDataCount = 0

		if n < 100 && strings.HasPrefix(string(buffer[:n]), "ERROR:") {
			fmt.Println(string(buffer[:n]))
			return
		}

		_, err = bufWriter.Write(buffer[:n])
		if err != nil {
			fmt.Println("Error writing to file:", err)
			return
		}

		totalBytes += n

		if time.Since(lastUpdate) > 100*time.Millisecond {
			elapsed := time.Since(start).Seconds()
			speed := float64(totalBytes) / (1024 * 1024 * elapsed)
			fmt.Printf("\rReceived: %d bytes (%.2f MB/s)", totalBytes, speed)
			lastUpdate = time.Now()
		}
	}

	bufWriter.Flush()

	elapsed := time.Since(start).Seconds()
	speed := float64(totalBytes) / (1024 * 1024 * elapsed)
	fmt.Printf("\nFile '%s' downloaded successfully (%d bytes in %.2f seconds, %.2f MB/s)\n",
		filename, totalBytes, elapsed, speed)
}