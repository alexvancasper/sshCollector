package main

import (
    "log"
    "net"
    "os"
)

func echoServer(c net.Conn) {
    for {
        buf := make([]byte, 65535)
        nr, err := c.Read(buf)
        if err != nil {
            return
        }

        data := buf[0:nr]
        println("Server got:", string(data))
        // _, err = c.Write(data)
        // if err != nil {
        //     log.Fatal("Write: ", err)
        // }
    }
}

func main() {
    l, err := net.Listen("unix", "/tmp/alarm.sock")
    if err != nil {
        log.Fatal("listen error:", err)
    }
    defer os.Remove("/tmp/alarm.sock")

    for {
        fd, err := l.Accept()
        if err != nil {
            log.Fatal("accept error:", err)
        }

        go echoServer(fd)
    }

}
