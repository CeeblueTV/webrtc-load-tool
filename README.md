# webrtc-load-tool
Tool for simulating WebRTC viewers and load testing WebRTC streams

**webrtc-load-tool v0.0.0**  

## Usage  
```sh
webrtc-load-tool WHIP-URL [flags]
```

## Flags  
- `-b, --buffersize duration`   
Buffer size for RTP jitter buffer for lost packets counter (default 500ms)
- `-c, --connections string`  
  Maximum number of connections to create (default: `"1"`)  
- `-d, --duration duration`  
  Time to run the test (default: `1m0s`)  
- `-l, --lite`  
Lite mode, no Video or Audio handling
- `-m, --relaymode string`  
  Relay mode to use (`auto`, `no`, `only`) (default: `"auto"`)  
- `-r, --runup duration`  
  Time frame to create the maximum number of connections  

## Example  
```sh
webrtc-load-tool http://ceeblue.net -c 100 -r 10s -d 1m
```
