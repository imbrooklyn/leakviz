package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: chanrecv PROFILE_PATH")
	}

	ready := make(chan struct{})
	go pprof.Do(
		context.Background(),
		pprof.Labels("scenario", "channel_receive", "tenant", "real-inline"),
		func(context.Context) {
			leakChannelReceive(ready)
		},
	)
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

func leakChannelReceive(ready chan<- struct{}) {
	blocked := make(chan struct{})
	close(ready)
	receiveInline(blocked)
}

func receiveInline(blocked <-chan struct{}) {
	<-blocked
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
