package main

import "errors"

var (
	errShortArgs          = errors.New("short args")
	errParseFlags         = errors.New("could not parse flags")
	errWhipURLRequired    = errors.New("whip URL is required")
	errInvalidWhipURL     = errors.New("invalid whip URL")
	errInvalidConnections = errors.New("invalid connections")
	errInvalidNumber      = errors.New("invalid number")
	errInvalidDuration    = errors.New("invalid test duration")
	errInvalidRelayMode   = errors.New("invalid relay mode")
)
