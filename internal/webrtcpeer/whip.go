package webrtcpeer

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Basic whip implementation, It doesn't hanlde edge cases,
// And doesn't implement the full protocol, But it's enough
// For Ceeblue whip server.
func whip(ctx context.Context, client *http.Client, url, offer string) (answer string, err error) {
	req, err := buildWhipRequest(ctx, url, offer)
	if err != nil {
		return "", fmt.Errorf("%w: failed to create request", err)
	}

	defer req.Body.Close() //nolint:errcheck // Should never happen

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: failed to send request", err)
	}

	defer func() {
		if res.Body != nil {
			_ = res.Body.Close()
		}
	}()

	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("%w: invalid WHIP response code %d", errInvalidWHIPResponseCode, res.StatusCode)
	}

	mimeType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if mimeType != "application/sdp" {
		return "", fmt.Errorf("%w: invalid WHIP response", errInvalidWhipResponse)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("%w: failed to read response", err)
	}

	if len(body) == 0 {
		return "", fmt.Errorf("%w: empty response", errInvalidWhipResponse)
	}

	return string(body), nil
}

func buildWhipRequest(ctx context.Context, url, offer string) (*http.Request, error) {
	if offer == "" {
		return nil, fmt.Errorf("%w: Offer SDP is empty", errEmptySDP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(offer))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("Accept", "application/sdp")

	return req, nil
}
