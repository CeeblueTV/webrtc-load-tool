package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testurl = "http://ceeblue.net"

func TestParseFlags_Invalid(t *testing.T) {
	f := initFlags()

	assert.Error(t, f.Parse([]string{"webrtc-load-tool", "http://ceeblue.net", "--wrongflag"}))
	assert.ErrorIs(t, f.Parse([]string{}), errShortArgs)
	assert.ErrorIs(t, (&flags{}).Parse([]string{"webrtc-load-tool"}), errParseFlags)
}

func TestParseFlags_WhipURL(t *testing.T) {
	flags := initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool"}), errWhipURLRequired)

	flags = initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", ":-3"}), errInvalidWhipURL)

	flags = initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", "ws://ceeblue.net"}), errInvalidWhipURL)

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, testurl, flags.WhipEndpoint)
}

func TestParseFlags_ParseHumanReadableNumber(t *testing.T) {
	flags := &flags{}
	num, err := flags.parseHumanReadableNumber("1")
	assert.NoError(t, err)
	assert.Equal(t, uint(1), num)

	num, err = flags.parseHumanReadableNumber("1k")
	assert.NoError(t, err)
	assert.Equal(t, uint(1000), num)

	num, err = flags.parseHumanReadableNumber("1K")
	assert.NoError(t, err)
	assert.Equal(t, uint(1000), num)

	num, err = flags.parseHumanReadableNumber("0")
	assert.NoError(t, err)
	assert.Equal(t, uint(0), num)

	num, err = flags.parseHumanReadableNumber("0k")
	assert.NoError(t, err)
	assert.Equal(t, uint(0), num)

	num, err = flags.parseHumanReadableNumber("1.2k")
	assert.NoError(t, err)
	assert.Equal(t, uint(1200), num)

	num, err = flags.parseHumanReadableNumber("1.123k")
	assert.NoError(t, err)
	assert.Equal(t, uint(1123), num)

	num, err = flags.parseHumanReadableNumber("1.1234k")
	assert.NoError(t, err)
	assert.Equal(t, uint(1123), num)

	num, err = flags.parseHumanReadableNumber("11_23-4,5.678k")
	assert.NoError(t, err)
	assert.Equal(t, uint(112345678), num)

	num, err = flags.parseHumanReadableNumber("11 33")
	assert.ErrorIs(t, err, errInvalidNumber)
	assert.Equal(t, uint(0), num)
	num, err = flags.parseHumanReadableNumber("11k3")
	assert.ErrorIs(t, err, errInvalidNumber)
	assert.Equal(t, uint(0), num)
	num, err = flags.parseHumanReadableNumber("11.33.3k")
	assert.ErrorIs(t, err, errInvalidNumber)
	assert.Equal(t, uint(0), num)
	num, err = flags.parseHumanReadableNumber("11333a")
	assert.ErrorIs(t, err, errInvalidNumber)
	assert.Equal(t, uint(0), num)
}

func TestParseFlags_praseConnections(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, uint(1), flags.Connections, "default connections should be 1")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-c", "3"}))
	assert.Equal(t, uint(3), flags.Connections)

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--connections", "1k"}))
	assert.Equal(t, uint(1000), flags.Connections)

	flags = initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--connections", ""}), errInvalidConnections)
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--connections", "2..2k"}), errInvalidNumber)
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--connections", "0"}), errInvalidConnections)
	flags.connectionsVal = nil
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--connections", "0"}), errInvalidConnections)
}

func TestParseFlags_praseDuration(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, time.Minute, flags.Duration, "default duration should be one minute")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-d", "3s"}))
	assert.Equal(t, time.Second*3, flags.Duration)

	flags = initFlags()
	assert.Error(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--duration", "1m3"}))
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--duration", "0"}), errInvalidDuration)
}

func TestParseFlags_praseRunup(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, time.Minute, flags.Duration, "default duration should be one minute")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-d", "3s"}))
	assert.Equal(t, time.Second*3, flags.Duration)

	flags = initFlags()
	assert.Error(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--duration", "1m3"}))
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--duration", "0"}), errInvalidDuration)
}

func TestParseFlags_parseRelayMode(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, RelayModeAuto, flags.RelayMode, "default relay mode should be auto")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-m", "no"}))
	assert.Equal(t, RelayModeNo, flags.RelayMode)

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "only"}))
	assert.Equal(t, RelayModeOnly, flags.RelayMode)

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "ONLY"}))
	assert.Equal(t, RelayModeOnly, flags.RelayMode)

	flags = initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "unknown"}), errInvalidRelayMode)
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", ""}), errInvalidRelayMode)

	flags = initFlags()
	flags.relayModeVal = nil
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", ""}), errInvalidRelayMode)
}

func TestParseFlags_parseLiteMode(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.Equal(t, false, flags.LiteMode, "default lite mode should be disabled by default")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-l"}))
	assert.Equal(t, true, flags.LiteMode)

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--lite"}))
	assert.Equal(t, true, flags.LiteMode)
}

func TestFlagSet(t *testing.T) {
	assert.Equal(t, "auto", RelayModeAuto.String())
	assert.Equal(t, "no", RelayModeNo.String())
	assert.Equal(t, "only", RelayModeOnly.String())
	assert.Equal(t, "unknown", relayMode(42).String())
}

func TestRelayModeICEServers(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "only"}))
	turnIce := flags.ICEServers()
	assert.NotEmpty(t, turnIce)
	for _, server := range turnIce {
		assert.NotEmpty(t, server.Username)
		assert.NotEmpty(t, server.Credential)
		for _, url := range server.URLs {
			assert.Equal(t, "turn:", url[:5])
		}
	}

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "no"}))
	stunIce := flags.ICEServers()
	assert.NotEmpty(t, stunIce)
	for _, server := range stunIce {
		for _, url := range server.URLs {
			assert.Equal(t, "stun:", url[:5])
		}
	}

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--relaymode", "auto"}))
	ice := flags.ICEServers()
	assert.GreaterOrEqual(t, len(ice), 1)
	assert.ElementsMatch(t, append(stunIce, turnIce...), ice)
}

func TestParseFlags_praseBufferSize(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl}))
	assert.NotZero(t, flags.BufferSize, "default buffer size should not be zero")

	flags = initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-b", "3s"}))
	assert.Equal(t, time.Second*3, flags.BufferSize)

	flags = initFlags()
	assert.Error(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--buffersize", "1m3"}))
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--buffersize", "0"}), errInvalidDuration)

	flags = initFlags()
	assert.ErrorIs(t, flags.Parse([]string{"webrtc-load-tool", testurl, "--buffersize", "10ms"}), errInvalidDuration)
}

func TestFlagsRunnerConfig(t *testing.T) {
	flags := initFlags()
	assert.NoError(t, flags.Parse([]string{"webrtc-load-tool", testurl, "-c", "3", "-d", "3s", "-l", "-b", "5s"}))
	config := flags.RunnerConfig()
	assert.Equal(t, testurl, config.WhipEndpoint)
	assert.Equal(t, uint(3), config.Connections)
	assert.Equal(t, time.Second*3, config.Duration)
	assert.Equal(t, time.Duration(0), config.Runup)
	assert.Equal(t, time.Second*5, config.BufferSize)
	assert.True(t, config.LiteMode)
	assert.NotEmpty(t, config.ICEServers)
}
