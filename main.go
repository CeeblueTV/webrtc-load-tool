// Description: WebRTC-load-tool is a tool for load testing WebRTC servers.
// By using this tool, you can simulate a large number of clients connecting to a WebRTC server.
// via WHIP signaling.
package main

import (
	"fmt"
	"os"
)

func main() {
	flags := initFlags()
	args := os.Args

	if len(args) <= 1 || args[1] == "-h" || args[1] == "--help" {
		fmt.Println("webrtc-load-tool v0.0.0")
		fmt.Println("Usage: webrtc-load-tool WHIP-URL [flags]")
		fmt.Println("Flags:")
		flags.PrintDefaults()
		fmt.Println("Example: webrtc-load-tool http://ceeblue.net -c 100 -r 10s -d 1m")

		return
	}

	if err := flags.Parse(args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
