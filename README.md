# webrtc-load-tool
Tool for simulating WebRTC viewers and load testing WebRTC streams

webrtc-load-tool v0.0.0

Usage: `webrtc-load-tool WHIP-URL [flags]`
Flags:
  -c, --connections string   maximum number of connections to create (default "1")
  -d, --duration duration    time to run the test (default 1m0s)
  -m, --relaymode string     relay mode to use (auto, no, only) (default "auto")
  -r, --runup duration       time frame to create maximum number of connections
Example: webrtc-load-tool http://ceeblue.net -c 100 -r 10s -d 1m
