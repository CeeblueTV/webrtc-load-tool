// Description: WebRTC-load-tool is a tool for load testing WebRTC servers.
// By using this tool, you can simulate a large number of clients connecting to a WebRTC server.
// via WHIP signaling.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/CeeblueTV/webrtc-load-tool/internal/runner"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func isHelp(args []string, fl *flags) bool {
	if len(args) <= 1 || args[1] == "-h" || args[1] == "--help" {
		fmt.Println("webrtc-load-tool v0.0.0")
		fmt.Println("Usage: webrtc-load-tool WHIP-URL [flags]")
		fmt.Println("Flags:")
		fl.PrintDefaults()
		fmt.Println("Example: webrtc-load-tool http://ceeblue.net -c 100 -r 10s -d 1m")

		return true
	}

	return false
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	flags := initFlags()
	args := os.Args

	if isHelp(args, flags) {
		os.Exit(0)
	}

	if err := flags.Parse(args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	runner, err := runner.New(flags.RunnerConfig())
	if err != nil {
		fmt.Println("Failed to create runner:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ok := runner.Run(ctx)

		if !ok {
			log.Info().Msg("Test run stopped")
			os.Exit(1)
		}

		log.Info().Msg("Test run completed")
		os.Exit(0)
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	gotFirstSignal := false
	for e := range c {
		if e != os.Interrupt {
			continue
		}

		if gotFirstSignal {
			log.Info().Msg("Forcing test run to stop")
			os.Exit(1)
		}

		log.Info().Msg("gracefully stopping test run")
		cancel()
		gotFirstSignal = true
	}
}
