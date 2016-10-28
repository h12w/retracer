package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
)

func main() {
	pid, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(processExists(pid))
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		// non-unix system
		return false
	}
	return nil == process.Signal(syscall.Signal(0))
}
