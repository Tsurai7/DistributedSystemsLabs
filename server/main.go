package main

import (
    "fmt"
    "log"
    "net"
    "server/handlers"
)

const (
    hostPort = ":8080"
    buffer   = 1024
)

func main() {
    ln, err := net.Listen("tcp", hostPort)
    if err != nil {
        log.Fatalf("Failed to listen: %v", err)
    }
    defer ln.Close()

    fmt.Printf("Server listening on %s\n", hostPort)

    for {
        conn, err := ln.Accept()
        if err != nil {
            log.Printf("Accept error: %v", err)
            continue
        }
        go handlers.HandleTcpConnection(conn)
    }
}