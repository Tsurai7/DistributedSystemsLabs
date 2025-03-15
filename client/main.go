package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
    "os"
    "client/handlers"
)

const(
	TcpServerAddr = "localhost:8081"
	UdpServerAddr = "localhost:9091"
)

func main() {
    conn, err := net.Dial("tcp", TcpServerAddr)
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    reader := bufio.NewReader(conn)
    writer := bufio.NewWriter(os.Stdout)
    scanner := bufio.NewScanner(os.Stdin)

    msg, err := reader.ReadString('\n')
    if err != nil {
        log.Fatal(err)
    }
    writer.WriteString(msg)
    writer.Flush()

    for {
        fmt.Print("> ")
        scanner.Scan()
        cmd := scanner.Text()
        if cmd == "exit" {
            return
        }

        fmt.Fprintf(conn, "%s\n", cmd)
        writer.WriteString(fmt.Sprintf("Sent command: %s\n", cmd))
        writer.Flush()

        switch cmd {
        case "echo":
            handlers.EchoMode(conn, reader, writer)
        case "upload":
            handlers.UploadFile(conn, reader, writer)
        case "download":
            handlers.DownloadFile(conn, reader, writer)
        default:
            msg, err := reader.ReadString('\n')
            if err != nil {
                log.Fatal(err)
            }
            writer.WriteString(msg)
            writer.Flush()
        }
    }
}