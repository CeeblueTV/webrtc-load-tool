package main

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/CeeblueTV/webrtc-load-tool/internal/runner"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

// represents webrtc-load-tool command line flags.
type flags struct {
	flagset             *pflag.FlagSet
	connectionsVal      *string
	runupVal            *time.Duration
	durationVal         *time.Duration
	relayModeVal        *string
	liteModeVal         *bool
	bufferDuration      *time.Duration
	targetResolutionVal *string
	targetCodecVal      *string
	// whipURL is the URL of the WHIP server.
	WhipEndpoint string
	// Connections is the maximum number of connections to create.
	// Default is 1.
	Connections uint
	// Runup is the time frame to create maximum number of connections.
	// wait between each connection creation = Runup / Connections
	// Default is 0.
	Runup time.Duration
	// Duration is the time to run the test, default is 1 minute.
	Duration time.Duration
	// RelayMode is the relay mode to use, default is auto.
	// possible values: auto, no, only
	RelayMode relayMode
	// LiteMode If enabled no Video or Audio handling.
	LiteMode bool
	// BufferDuration is the buffer duration for RTP jitter buffer for lost packets counter.
	BufferDuration time.Duration
	// TargetResolution is the target resolution height (e.g., 2160 for 4K, 1080 for HD, 720 for HD).
	// The client will automatically switch to the closest available resolution <= target.
	// Set to 0 to disable automatic resolution switching.
	TargetResolution int
	// TargetCodec is the preferred codec (e.g., "H264", "VP8").
	// If resolution is set but codec is not, defaults to H264.
	// Empty means any codec when resolution is also not set.
	TargetCodec string
}

type relayMode int

const (
	// RelayModeAuto use STUN (Google's public STUN server) and TURN (Ceeblue's TURN server)
	// For connections (client will determine the best connection method).
	RelayModeAuto relayMode = iota
	// RelayModeNo use STUN (Google's public STUN server) only.
	RelayModeNo
	// RelayModeOnly use TURN (Ceeblue's TURN server) only.
	RelayModeOnly
)

func (r relayMode) String() string {
	switch r {
	case RelayModeAuto:
		return "auto"
	case RelayModeNo:
		return "no"
	case RelayModeOnly:
		return "only"
	default:
		return "unknown"
	}
}

func initFlags() *flags {
	flagset := pflag.NewFlagSet("webrtc-load-tool", pflag.ContinueOnError)
	flagSet := &flags{
		flagset:        flagset,
		connectionsVal: flagset.StringP("connections", "c", "1", "maximum number of connections to create"),
		runupVal:       flagset.DurationP("runup", "r", 0, "time frame to create maximum number of connections"),
		durationVal:    flagset.DurationP("duration", "d", time.Minute, "time to run the test"),
		relayModeVal:   flagset.StringP("relaymode", "m", "auto", "relay mode to use (auto, no, only)"),
		liteModeVal:    flagset.BoolP("lite", "l", false, "lite mode, no Video or Audio handling"),
		bufferDuration: flagset.DurationP(
			"bufferduration", "b", time.Millisecond*500,
			"Buffer duration for RTP jitter buffer for lost packets counter",
		),
		targetResolutionVal: flagset.StringP(
			"targetresolution", "t", "",
			"target resolution (e.g., 4k, 1080p, 720p, 1080, 720). "+
				"Automatically switches to closest available resolution <= target",
		),
		targetCodecVal: flagset.StringP(
			"codec", "", "",
			"preferred codec (H264 or VP8). Defaults to H264 if resolution is set",
		),
	}

	return flagSet
}

// PrintDefaults Prints the flag set usage.
func (flagSet *flags) PrintDefaults() {
	flagSet.flagset.PrintDefaults()
}

// ParseFlags parses webrtc-load-tool command line flags.
func (flagSet *flags) Parse(args []string) error { //nolint:cyclop
	if len(args) == 0 {
		return errShortArgs
	}

	if flagSet.flagset == nil {
		return fmt.Errorf("%w: flags was not initialized", errParseFlags)
	}

	err := flagSet.flagset.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %s", errParseFlags, err) //nolint:errorlint // we wrap the error
	}

	if err := flagSet.parseWhipURL(); err != nil {
		return err
	}

	if err := flagSet.praseConnections(); err != nil {
		return err
	}

	if err := flagSet.parseRelayMode(); err != nil {
		return err
	}

	if flagSet.runupVal != nil {
		flagSet.Runup = *flagSet.runupVal
	}

	flagSet.Duration = time.Minute
	if flagSet.durationVal != nil {
		flagSet.Duration = *flagSet.durationVal
		if flagSet.Duration <= 0 {
			return fmt.Errorf("%w: duration must be greater than 0", errInvalidDuration)
		}
	}

	if flagSet.liteModeVal != nil {
		flagSet.LiteMode = *flagSet.liteModeVal
	}

	if err := flagSet.parseBufferDuration(); err != nil {
		return err
	}

	if err := flagSet.parseTargetResolution(); err != nil {
		return err
	}

	if err := flagSet.parseTargetCodec(); err != nil {
		return err
	}

	return nil
}

func (flagSet *flags) parseWhipURL() error {
	whip := flagSet.flagset.Arg(1)
	if whip == "" {
		return errEndpointRequired
	}

	parsedURL, err := url.Parse(whip)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidEndpointURL, err) //nolint:errorlint // we wrap the error
	}

	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" &&
		parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return fmt.Errorf("%w: scheme must be http(s) or ws(s)", errInvalidEndpointURL)
	}

	flagSet.WhipEndpoint = whip

	return nil
}

func (flagSet *flags) praseConnections() error {
	if flagSet.connectionsVal == nil {
		return fmt.Errorf("%w: missing connections value", errInvalidConnections)
	}

	connections := *flagSet.connectionsVal
	if connections == "" {
		flagSet.Connections = 1

		return fmt.Errorf("%w: missing connections value", errInvalidConnections)
	}

	cons, err := flagSet.parseHumanReadableNumber(connections)
	if err != nil {
		return err
	}

	if cons <= 0 {
		return fmt.Errorf("%w: connections must be greater than 0", errInvalidConnections)
	}

	flagSet.Connections = cons

	return nil
}

// parse SI unit (k) and allow for numbers seporators, and simple decimals.
func (flagSet *flags) parseHumanReadableNumber(num string) (uint, error) {
	cons := uint(0)
	dec := uint(0)
	hasDot := false
	for i, chr := range num {
		switch chr {
		case '.':
			if hasDot {
				return 0, fmt.Errorf("%w: invalid connections unexpected double dot", errInvalidNumber)
			}
			hasDot = true
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if hasDot {
				dec *= 10
				dec += uint(chr - '0')

				continue
			}

			cons *= 10
			cons += uint(chr - '0')
		case '-', ',', '_':
			// do nothing
		case 'k', 'K':
			if i != len(num)-1 {
				return 0, fmt.Errorf("%w: invalid connections unexpected character after k", errInvalidNumber)
			}

			connectionValues := 1000 * (float64(cons) + (float64(dec) / math.Pow(10, float64(len(fmt.Sprint(dec))))))
			cons = uint(connectionValues)
		default:
			return 0, fmt.Errorf("%w: invalid connections unexpected character %c", errInvalidNumber, chr)
		}
	}

	return cons, nil
}

func (flagSet *flags) parseRelayMode() error {
	if flagSet.relayModeVal == nil {
		return fmt.Errorf("%w: missing relay mode value", errInvalidRelayMode)
	}

	relay := strings.ToLower(*flagSet.relayModeVal)
	switch relay {
	case "auto":
		flagSet.RelayMode = RelayModeAuto
	case "no":
		flagSet.RelayMode = RelayModeNo
	case "only":
		flagSet.RelayMode = RelayModeOnly
	default:
		return fmt.Errorf("%w: invalid relay mode", errInvalidRelayMode)
	}

	return nil
}

func (flagSet *flags) parseBufferDuration() error {
	if flagSet.bufferDuration == nil {
		return fmt.Errorf("%w: missing buffer duration value", errInvalidDuration)
	}

	if *flagSet.bufferDuration <= time.Millisecond*10 {
		return fmt.Errorf("%w: buffer duration must be greater than 10ms", errInvalidDuration)
	}

	flagSet.BufferDuration = *flagSet.bufferDuration

	return nil
}

func (flagSet *flags) parseTargetResolution() error { //nolint:cyclop
	if flagSet.targetResolutionVal == nil || *flagSet.targetResolutionVal == "" {
		flagSet.TargetResolution = 0

		return nil
	}

	resStr := strings.ToLower(strings.TrimSpace(*flagSet.targetResolutionVal))

	resolution := parseResolutionName(resStr)
	if resolution > 0 {
		flagSet.TargetResolution = resolution

		return nil
	}

	resStr = strings.TrimSuffix(resStr, "p")

	var height int
	if _, err := fmt.Sscanf(resStr, "%d", &height); err != nil {
		return fmt.Errorf(
			"%w: invalid target resolution format, expected format like 4k, 1080p, 720p, 1080, or 720",
			errInvalidResolution,
		)
	}

	if height <= 0 {
		return fmt.Errorf("%w: target resolution must be greater than 0", errInvalidResolution)
	}

	flagSet.TargetResolution = height

	return nil
}

func parseResolutionName(resStr string) int {
	switch resStr {
	case "4k", "2160p", "2160":
		return 2160
	case "2k", "1440p", "1440":
		return 1440
	case "1080p", "1080", "hd", "fhd":
		return 1080
	case "720p", "720", "hdready":
		return 720
	case "480p", "480":
		return 480
	case "360p", "360":
		return 360
	case "240p", "240":
		return 240
	default:
		return 0
	}
}

func (flagSet *flags) parseTargetCodec() error {
	if flagSet.targetCodecVal == nil || *flagSet.targetCodecVal == "" {
		flagSet.TargetCodec = ""

		return nil
	}

	codec := strings.ToUpper(strings.TrimSpace(*flagSet.targetCodecVal))
	switch codec {
	case "H264", "H.264":
		flagSet.TargetCodec = "H264"
	case "VP8":
		flagSet.TargetCodec = "VP8"
	default:
		return fmt.Errorf("%w: invalid codec, expected H264 or VP8", errInvalidResolution)
	}

	return nil
}

func (flagSet *flags) ICEServers() []webrtc.ICEServer {
	stun := []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	}

	if flagSet.RelayMode == RelayModeNo {
		return stun
	}

	turn, err := flagSet.turnICEServers()
	if err != nil {
		log.Error().Err(err).Msg("failed to get TURN servers, using stun only")

		return stun
	}

	if flagSet.RelayMode == RelayModeAuto {
		return append(stun, turn...)
	}

	return turn
}

func (flagSet *flags) turnICEServers() ([]webrtc.ICEServer, error) {
	parsedURL, err := url.Parse(flagSet.WhipEndpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errInvalidEndpointURL, err) //nolint:errorlint // we wrap the error
	}

	domain := parsedURL.Hostname()

	return []webrtc.ICEServer{
		{
			URLs: []string{
				"turn:" + domain + ":3478",
				"turn:" + domain + ":3478?transport=tcp",
			},
			Username:   "ceeblue",
			Credential: "ceeblue",
		},
	}, nil
}

func (flagSet *flags) RunnerConfig() runner.Config {
	transportPolicy := webrtc.ICETransportPolicyAll
	if flagSet.RelayMode == RelayModeOnly {
		transportPolicy = webrtc.ICETransportPolicyRelay
	}

	return runner.Config{
		ICEServers:         flagSet.ICEServers(),
		ICETransportPolicy: transportPolicy,
		LiteMode:           flagSet.LiteMode,
		WhipEndpoint:       flagSet.WhipEndpoint,
		Connections:        flagSet.Connections,
		Runup:              flagSet.Runup,
		Duration:           flagSet.Duration,
		BufferDuration:     flagSet.BufferDuration,
		TargetResolution:   flagSet.TargetResolution,
		TargetCodec:        flagSet.TargetCodec,
	}
}
