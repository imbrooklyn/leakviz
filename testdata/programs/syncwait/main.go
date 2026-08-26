package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: syncwait PROFILE_PATH")
	}

	ready := make(chan struct{})
	go leakWaitGroup(ready)
	<-ready

	if _, err := fmt.Fprintln(os.Stdout, "READY"); err != nil {
		return fmt.Errorf("write ready handshake: %w", err)
	}
	command, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read capture command: %w", err)
	}
	if strings.TrimSpace(command) != "capture" {
		return fmt.Errorf("unexpected command")
	}

	return writeLeakProfile(os.Args[1])
}

func leakWaitGroup(ready chan<- struct{}) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	close(ready)
	waitGroup.Wait()
}

func writeLeakProfile(path string) error {
	profile := pprof.Lookup("goroutineleak")
	if profile == nil {
		return fmt.Errorf("goroutineleak profile is unavailable")
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	if err := profile.WriteTo(file, 0); err != nil {
		_ = file.Close()
		return fmt.Errorf("write profile: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close profile: %w", err)
	}
	return nil
}
