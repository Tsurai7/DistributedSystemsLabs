package handlers

import (
    "bufio"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "strings"
    "time"
)

func HandleTcpConnection(conn net.Conn) {
    defer conn.Close()
    fmt.Printf("New connection from %s\n", conn.RemoteAddr())

    reader := bufio.NewReader(conn)
    writer := bufio.NewWriter(conn)

    for {
        // Read command
        cmd, err := reader.ReadString('\n')
        if err != nil {
            if err != io.EOF {
                log.Printf("Read error: %v", err)
            }
            return
        }
        cmd = strings.TrimSpace(cmd)

        // Handle commands
        switch cmd {
        case "time":
            writer.WriteString(fmt.Sprintf("Current time: %v\n", time.Now().Format(time.RFC3339)))
        case "echo":
            writer.WriteString("Echo mode activated. Type 'exit' to quit.\n")
            echoMode(reader, writer)
        case "upload":
            writer.WriteString("Upload mode activated. Send file name and data.\n")
            uploadFile(reader, writer)
        case "download":
            writer.WriteString("Download mode activated. Send file name.\n")
            downloadFile(reader, writer)
        default:
            writer.WriteString("Invalid command\n")
        }

        writer.Flush()
    }
}

func echoMode(reader *bufio.Reader, writer *bufio.Writer) {
    for {
        msg, err := reader.ReadString('\n')
        if err != nil {
            return
        }
        msg = strings.TrimSpace(msg)
        if msg == "exit" {
            writer.WriteString("Exiting echo mode\n")
            writer.Flush()
            return
        }
        writer.WriteString("Echo: " + msg + "\n")
        writer.Flush()
    }
}

func uploadFile(reader *bufio.Reader, writer *bufio.Writer) {
    // Read file name
    fileName, err := reader.ReadString('\n')
    if err != nil {
        writer.WriteString("Upload failed\n")
        writer.Flush()
        return
    }
    fileName = strings.TrimSpace(fileName)

    // Read file data
    data, err := reader.ReadString('\n')
    if err != nil {
        writer.WriteString("Upload failed\n")
        writer.Flush()
        return
    }

    // Save file
    err = os.WriteFile(fileName, []byte(data), 0644)
    if err != nil {
        writer.WriteString("Upload failed\n")
        writer.Flush()
        return
    }
    writer.WriteString("File uploaded successfully\n")
    writer.Flush()
}

func downloadFile(reader *bufio.Reader, writer *bufio.Writer) {
    // Read file name
    fileName, err := reader.ReadString('\n')
    if err != nil {
        writer.WriteString("Download failed\n")
        writer.Flush()
        return
    }
    fileName = strings.TrimSpace(fileName)

    // Read file data
    data, err := os.ReadFile(fileName)
    if err != nil {
        writer.WriteString("File not found\n")
        writer.Flush()
        return
    }

    // Send file data
    writer.WriteString(string(data) + "\n")
    writer.Flush()
}
