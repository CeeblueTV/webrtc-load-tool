# webrtc-load-tool
Tool for simulating WebRTC viewers and load testing WebRTC streams

**webrtc-load-tool v1.0.0**

## Usage
```sh
webrtc-load-tool WHIP-URL [flags]
```

## Flags
- `-b, --bufferduration duration`
  Buffer duration for RTP jitter buffer for lost packets counter (default 500ms)
- `-c, --connections string`
  Maximum number of connections to create (default: `"1"`)
- `--codec string`
  Preferred codec (`H264` or `VP8`). Defaults to H264 if resolution is set
- `-d, --duration duration`
  Time to run the test (default: `1m0s`)
- `-l, --lite`
  Lite mode, no RTP video/audio parsing
- `-m, --relaymode string`
  Relay mode to use (`auto`, `no`, `only`) (default: `"auto"`)
- `-r, --runup duration`
  Time frame to create the maximum number of connections
- `-t, --targetresolution string`
  Target resolution height. Automatically switches to the closest available resolution <= target.
  (websocket mode only)
  Accepts: `4k`, `2160p`, `2160`, `2k`, `1440p`, `1440`, `1080p`, `1080`, `hd`, `fhd`,
  `720p`, `720`, `hdready`, `480p`, `480`, `360p`, `360`, `240p`, `240`, or a numeric value.
  Set to empty to disable resolution switching.

## Example
```sh
webrtc-load-tool https://[node].ceeblue.tv/webrtc/[streamid]?video=3 -c 10 -r 10s -d 1m
```

target resoultion (Ceeblue websocket protocol):

```sh
webrtc-load-tool wss://[node].ceeblue.tv/webrtc/[streamid] -c 1 -r 10s -d 1m -t 720p
```
