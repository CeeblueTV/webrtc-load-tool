package main

import "errors"

var (
	errShortArgs          = errors.New("short args")
	errParseFlags         = errors.New("could not parse flags")
	errEndpointRequired   = errors.New("endpoint URL is required")
	errInvalidEndpointURL = errors.New("invalid endpoint URL")
	errInvalidConnections = errors.New("invalid connections")
	errInvalidNumber      = errors.New("invalid number")
	errInvalidDuration    = errors.New("invalid test duration")
	errInvalidRelayMode   = errors.New("invalid relay mode")
	errInvalidResolution  = errors.New("invalid target resolution")
)
