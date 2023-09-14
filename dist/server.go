package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func main() {
	sockFile := "/var/run/oss-server.sock"

	_, err := os.Stat(sockFile)
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}

	if err == nil {
		if err = os.Remove(sockFile); err != nil {
			panic(err)
		}
	}

	l, err := net.Listen("unix", sockFile)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				panic(err)
			}

			go handleConnection(conn)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	<-sigs

	fmt.Println("cleanup...")
	os.Exit(0)
}

func handleConnection(conn net.Conn) {
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(buf[:n]))
	cmd := exec.Command("sh", "-c", string(buf[:n]))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	codeStr := "Success"
	if _, err = cmd.CombinedOutput(); err != nil {
		fmt.Println(err)
		codeStr = "Fail"
	}

	_, err = conn.Write([]byte(codeStr))
	if err != nil {
		fmt.Println(err)
	}
}
