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
	flagset        *pflag.FlagSet
	connectionsVal *string
	runupVal       *time.Duration
	durationVal    *time.Duration
	relayModeVal   *string
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
	f := &flags{
		flagset:        flagset,
		connectionsVal: flagset.StringP("connections", "c", "1", "maximum number of connections to create"),
		runupVal:       flagset.DurationP("runup", "r", 0, "time frame to create maximum number of connections"),
		durationVal:    flagset.DurationP("duration", "d", time.Minute, "time to run the test"),
		relayModeVal:   flagset.StringP("relaymode", "m", "auto", "relay mode to use (auto, no, only)"),
	}

	return f
}

// PrintDefaults Prints the flag set usage.
func (f *flags) PrintDefaults() {
	f.flagset.PrintDefaults()
}

// ParseFlags parses webrtc-load-tool command line flags.
func (f *flags) Parse(args []string) error {
	if len(args) == 0 {
		return errShortArgs
	}

	if f.flagset == nil {
		return fmt.Errorf("%w: flags was not initialized", errParseFlags)
	}

	err := f.flagset.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %s", errParseFlags, err) //nolint:errorlint // we wrap the error
	}

	if err := f.parseWhipURL(); err != nil {
		return err
	}

	if err := f.praseConnections(); err != nil {
		return err
	}

	if err := f.parseRelayMode(); err != nil {
		return err
	}

	if f.runupVal != nil {
		f.Runup = *f.runupVal
	}

	f.Duration = time.Minute
	if f.durationVal != nil {
		f.Duration = *f.durationVal
		if f.Duration <= 0 {
			return fmt.Errorf("%w: duration must be greater than 0", errInvalidDuration)
		}
	}

	return nil
}

func (f *flags) parseWhipURL() error {
	whip := f.flagset.Arg(1)
	if whip == "" {
		return errWhipURLRequired
	}

	u, err := url.Parse(whip)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidWhipURL, err) //nolint:errorlint // we wrap the error
	}

	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: scheme must be http(s)", errInvalidWhipURL)
	}

	f.WhipEndpoint = whip

	return nil
}

func (f *flags) praseConnections() error {
	if f.connectionsVal == nil {
		return fmt.Errorf("%w: missing connections value", errInvalidConnections)
	}

	connections := *f.connectionsVal
	if connections == "" {
		f.Connections = 1

		return fmt.Errorf("%w: missing connections value", errInvalidConnections)
	}

	cons, err := f.parseHumanReadableNumber(connections)
	if err != nil {
		return err
	}

	if cons <= 0 {
		return fmt.Errorf("%w: connections must be greater than 0", errInvalidConnections)
	}

	f.Connections = cons

	return nil
}

// parse SI unit (k) and allow for numbers seporators, and simple decimals.
func (f *flags) parseHumanReadableNumber(num string) (uint, error) {
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

func (f *flags) parseRelayMode() error {
	if f.relayModeVal == nil {
		return fmt.Errorf("%w: missing relay mode value", errInvalidRelayMode)
	}

	relay := strings.ToLower(*f.relayModeVal)
	switch relay {
	case "auto":
		f.RelayMode = RelayModeAuto
	case "no":
		f.RelayMode = RelayModeNo
	case "only":
		f.RelayMode = RelayModeOnly
	default:
		return fmt.Errorf("%w: invalid relay mode", errInvalidRelayMode)
	}

	return nil
}

func (f *flags) ICEServers() []webrtc.ICEServer {
	stun := []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	}

	if f.RelayMode == RelayModeNo {
		return stun
	}

	turn, err := f.turnICEServers()
	if err != nil {
		log.Error().Err(err).Msg("failed to get TURN servers, using stun only")

		return stun
	}

	if f.RelayMode == RelayModeAuto {
		return append(stun, turn...)
	}

	return turn
}

func (f *flags) turnICEServers() ([]webrtc.ICEServer, error) {
	u, err := url.Parse(f.WhipEndpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errInvalidWhipURL, err) //nolint:errorlint // we wrap the error
	}

	domain := u.Hostname()

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

func (f *flags) RunnerConfig() runner.Config {
	transportPolicy := webrtc.ICETransportPolicyAll
	if f.RelayMode == RelayModeOnly {
		transportPolicy = webrtc.ICETransportPolicyRelay
	}

	return runner.Config{
		ICEServers:         f.ICEServers(),
		ICETransportPolicy: transportPolicy,
		WhipEndpoint:       f.WhipEndpoint,
		Connections:        f.Connections,
		Runup:              f.Runup,
		Duration:           f.Duration,
	}
}
