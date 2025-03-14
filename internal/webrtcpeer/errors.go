package webrtcpeer

import "errors"

var (
	errEmptySDP                = errors.New("empty sdp")
	errInvalidWHIPResponseCode = errors.New("invalid WHIP response code")
	errInvalidWhipResponse     = errors.New("invalid WHIP response")
)
