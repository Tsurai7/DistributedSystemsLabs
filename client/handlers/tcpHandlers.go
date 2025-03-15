package handlers

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
)

func EchoMode(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) {
    fmt.Println("Echo mode activated. Type 'exit' to quit.")
    scanner := bufio.NewScanner(os.Stdin)

    for {
        fmt.Print("> ")
        scanner.Scan()
        msg := scanner.Text()
        if msg == "exit" {
            fmt.Fprintln(conn, msg)
            return
        }

        fmt.Fprintf(conn, "%s\n", msg)
        response, err := reader.ReadString('\n')
        if err != nil {
            log.Fatal(err)
        }
        writer.WriteString(response)
        writer.Flush()
    }
}

func UploadFile(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) {
    fmt.Print("Enter file name: ")
    var fileName string
    scanner := bufio.NewScanner(os.Stdin)
    scanner.Scan()
    fileName = scanner.Text()

    fmt.Print("Enter file data: ")
    scanner.Scan()
    data := scanner.Text()

    fmt.Fprintf(conn, "%s\n", fileName)
    fmt.Fprintf(conn, "%s\n", data)

    response, err := reader.ReadString('\n')
    if err != nil {
        log.Fatal(err)
    }
    writer.WriteString(response)
    writer.Flush()
}


func DownloadFile(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer) {
    fmt.Print("Enter file name: ")
    var fileName string
    fmt.Scanln(&fileName)

    fmt.Fprintf(conn, "%s\n", fileName)

    response, err := reader.ReadString('\n')
    if err != nil {
        log.Fatal(err)
    }
    writer.WriteString(response)
    writer.Flush()
}
