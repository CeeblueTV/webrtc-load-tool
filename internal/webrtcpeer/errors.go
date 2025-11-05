package webrtcpeer

import "errors"

var (
	errEmptySDP                = errors.New("empty sdp")
	errInvalidHTTPResponseCode = errors.New("invalid HTTP response code")
	errInvalidHTTPResponse     = errors.New("invalid HTTP response")
)
