package webrtcpeer

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const wsAnswerTimeout = 20 * time.Second

var (
	errAccessDenied          = errors.New("access denied")
	errSignalingReaderClosed = errors.New("signaling reader closed")
	errTimeoutAnswer         = errors.New("timeout waiting for answer_sdp")
)

type wsResult struct {
	Value string
	Err   error
}

// resolutionTrack represents a media track and metadata for adaptive switching.
type resolutionTrack struct {
	Idx     int    `json:"idx"`     // index for switching
	Trackid int    `json:"trackid"` // track ID for logging
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Codec   string `json:"codec"` // e.g., "H264", "VP8"
	Type    string `json:"type"`  // "video" or "audio"
}

type metadataEnvelope struct {
	Meta struct {
		Tracks map[string]resolutionTrack `json:"tracks"`
	} `json:"meta"`
}

type resolutionUpdate struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type incomingEnvelope struct {
	Type string `json:"type"`
}

type answerEvent struct {
	Type      string `json:"type"`
	Result    bool   `json:"result"`
	AnswerSDP string `json:"answer_sdp"` //nolint:tagliatelle // API compatibility
}

type offerMessage struct {
	Type     string `json:"type"`
	OfferSDP string `json:"offer_sdp"` //nolint:tagliatelle // API compatibility
}

type setTracksMessage struct {
	Type  string `json:"type"` // "tracks"
	Video int    `json:"video"`
}

type resolutionTracker struct {
	targetHeight    int
	targetCodec     string // Preferred codec ("H264", "VP8"), empty means any
	availableTracks []resolutionTrack

	currentIdx     int
	lastSwitchTime time.Time
	switchCooldown time.Duration

	mu   sync.RWMutex
	conn *websocket.Conn
}

func newResolutionTracker(targetHeight int, targetCodec string, conn *websocket.Conn) *resolutionTracker {
	// Default to H264 if a resolution is requested but no codec preference given.
	if targetHeight > 0 && targetCodec == "" {
		targetCodec = "H264"
	}

	return &resolutionTracker{
		targetHeight:    targetHeight,
		targetCodec:     targetCodec,
		availableTracks: make([]resolutionTrack, 0),
		switchCooldown:  500 * time.Millisecond,
		conn:            conn,
	}
}

// findBestTrack computes the best index from the *current* available tracks.
func (rt *resolutionTracker) findBestTrack() (int, bool) {
	rt.mu.RLock()
	tracks := append([]resolutionTrack(nil), rt.availableTracks...)
	tgt := rt.targetHeight
	codec := rt.targetCodec
	rt.mu.RUnlock()

	return findBestTrackIndex(tracks, tgt, codec)
}

// updateTracks updates available tracks and (optionally) switches.
func (rt *resolutionTracker) updateTracks(tracks []resolutionTrack) {
	if len(tracks) == 0 {
		return
	}

	rt.mu.Lock()
	rt.availableTracks = append(rt.availableTracks[:0], tracks...)
	currentIdx := rt.currentIdx
	last := rt.lastSwitchTime
	cooldown := rt.switchCooldown
	conn := rt.conn
	targetHeight := rt.targetHeight
	targetCodec := rt.targetCodec
	rt.mu.Unlock()

	if targetHeight <= 0 {
		return
	}
	if time.Since(last) < cooldown {
		return
	}

	bestIdx, found := findBestTrackIndex(tracks, targetHeight, targetCodec)
	if !found || bestIdx == currentIdx {
		return
	}

	if conn == nil {
		return
	}

	// Send track switch; avoid holding lock while doing I/O.
	msg := setTracksMessage{Type: "tracks", Video: bestIdx}
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		log.Warn().Err(err).Msg("failed to set write deadline for track switch")

		return
	}
	if err := conn.WriteJSON(&msg); err != nil {
		log.Error().Err(err).Int("idx", bestIdx).Msg("failed to send track switch")

		return
	}

	rt.mu.Lock()
	rt.currentIdx = bestIdx
	rt.lastSwitchTime = time.Now()
	rt.mu.Unlock()
}

// findBestTrackIndex selects a track with height <= target closest to target; if none,
// choose the lowest available that matches the codec (if specified).
func findBestTrackIndex(tracks []resolutionTrack, targetHeight int, targetCodec string) (int, bool) { //nolint:cyclop
	if len(tracks) == 0 {
		return 0, false
	}

	bestIdx, found := findBestTrackAtOrBelow(tracks, targetHeight, targetCodec)
	if found {
		return bestIdx, true
	}

	return findLowestTrack(tracks, targetCodec)
}

func findBestTrackAtOrBelow(tracks []resolutionTrack, targetHeight int, targetCodec string) (int, bool) {
	best := resolutionTrack{Height: 0}
	found := false

	for _, t := range tracks {
		if !matchesCodec(t.Codec, targetCodec) {
			continue
		}
		if t.Height <= targetHeight && t.Height > best.Height {
			best = t
			found = true
		}
	}

	if !found {
		return 0, false
	}

	return best.Idx, true
}

func findLowestTrack(tracks []resolutionTrack, targetCodec string) (int, bool) {
	best := resolutionTrack{Height: 0}
	found := false

	for i := range tracks {
		t := tracks[i]
		if !matchesCodec(t.Codec, targetCodec) {
			continue
		}
		if !found || t.Height < best.Height {
			best = t
			found = true
		}
	}

	if !found {
		return 0, false
	}

	return best.Idx, true
}

func matchesCodec(codec, want string) bool {
	if want == "" {
		return true
	}

	return strings.EqualFold(codec, want)
}

type wsConnections struct {
	sigConn  *websocket.Conn
	metaConn *websocket.Conn
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func (w *wsConnections) close() {
	closeWS(w.sigConn)
	closeWS(w.metaConn)
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

func connectWebSockets(ctx context.Context, endpoint string) (*wsConnections, error) {
	ctx, cancel := context.WithCancel(ctx)

	sigURL, metaURL := deriveWSURLs(endpoint)
	log.Info().Str("signaling", sigURL).Str("metadata", metaURL).Msg("connecting WebSocket endpoints")

	sigConn, resp, err := websocket.DefaultDialer.DialContext(ctx, sigURL, nil)
	if resp != nil && resp.Body != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close signaling response body")
		}
	}
	if err != nil {
		cancel()

		return nil, err
	}

	metaConn, mresp, err := websocket.DefaultDialer.DialContext(ctx, metaURL, nil)
	if mresp != nil && mresp.Body != nil {
		if closeErr := mresp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close metadata response body")
		}
	}
	if err != nil {
		if closeErr := sigConn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close signaling connection after metadata dial error")
		}
		cancel()

		return nil, err
	}

	// Ignore remote close frames; readers will exit naturally.
	sigConn.SetCloseHandler(func(int, string) error { return nil })
	metaConn.SetCloseHandler(func(int, string) error { return nil })

	startKeepAlive(ctx, sigConn)
	startKeepAlive(ctx, metaConn)

	return &wsConnections{
		sigConn:  sigConn,
		metaConn: metaConn,
		cancel:   cancel,
	}, nil
}

// resolver ensures we only ever resolve the promise once.
type resolver struct {
	ch   chan<- wsResult
	once sync.Once
}

func (r *resolver) success(v string) { r.once.Do(func() { r.ch <- wsResult{Value: v} }) }
func (r *resolver) fail(err error)   { r.once.Do(func() { r.ch <- wsResult{Err: err} }) }

// handleSignalingMessages reads signaling, fulfills answer, and exits.
func handleSignalingMessages(conn *websocket.Conn, res *resolver) {
	defer func() {
		// If we exit without having resolved, signal an error once.
		res.fail(errSignalingReaderClosed)
	}()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			log.Warn().Err(err).Msg("failed to set read deadline in signaling handler")
			res.fail(err)

			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Debug().Err(err).Msg("signaling message read error")
			res.fail(err)

			return
		}

		var env incomingEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Warn().Err(err).Str("raw", string(data)).Msg("failed to unmarshal signaling envelope")

			continue
		}

		log.Info().Str("type", env.Type).Msg("signaling event")

		switch env.Type {
		case "on_answer_sdp":
			var ans answerEvent
			if err := json.Unmarshal(data, &ans); err != nil {
				log.Error().Err(err).Str("raw", string(data)).Msg("failed to unmarshal answer event")
				res.fail(err)

				return
			}
			if !ans.Result {
				res.fail(errAccessDenied)

				return
			}
			res.success(ans.AnswerSDP)

			return
		case "on_media_receive", "on_time", "on_playing":
			logResolutionUpdate(data)
		default:
			// ignore
		}
	}
}

func logResolutionUpdate(raw []byte) {
	var ru resolutionUpdate
	if err := json.Unmarshal(raw, &ru); err != nil {
		log.Warn().Err(err).Str("raw", string(raw)).Msg("failed to unmarshal resolution update")

		return
	}
	if ru.Width > 0 && ru.Height > 0 {
		log.Info().Int("width", ru.Width).Int("height", ru.Height).Msg("resolution update")
	}
}

// handleMetadataMessages reads metadata and updates the tracker.
func handleMetadataMessages(conn *websocket.Conn, tracker *resolutionTracker) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			log.Warn().Err(err).Msg("failed to set read deadline in metadata handler")
			log.Info().Err(err).Msg("metadata WS closed")

			return
		}

		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			log.Debug().Err(readErr).Msg("metadata message read error")
			log.Info().Err(readErr).Msg("metadata WS closed")

			return
		}

		log.Debug().Int("bytes", len(data)).Msg("metadata message")

		if tracker != nil {
			tracks := extractTracksFromMetadata(data)
			if len(tracks) > 0 {
				tracker.updateTracks(tracks)
			}
		}
	}
}

func waitForAnswer(ctx context.Context, promise <-chan wsResult, conns *wsConnections) (string, error) {
	// Context-local timeout takes precedence.
	ctx, cancel := context.WithTimeout(ctx, wsAnswerTimeout)
	defer cancel()

	select {
	case <-ctx.Done():
		conns.close()

		return "", errTimeoutAnswer
	case r := <-promise:
		if r.Err != nil {
			conns.close()

			return "", r.Err
		}

		return r.Value, nil
	}
}

func triggerInitialTrackCheck(tracker *resolutionTracker) {
	if tracker == nil {
		return
	}
	time.AfterFunc(1*time.Second, func() {
		tracker.mu.RLock()
		cp := append([]resolutionTrack(nil), tracker.availableTracks...)
		tracker.mu.RUnlock()
		if len(cp) > 0 {
			tracker.updateTracks(cp)
		}
	})
}

// wsSignal connects, sends offer_sdp, waits for on_answer_sdp, and returns the answer + closer.
func wsSignal(
	ctx context.Context,
	endpoint string,
	offer string,
	targetResolution int,
	targetCodec string,
) (string, func(), error) {
	if strings.TrimSpace(offer) == "" {
		return "", nil, errEmptySDP
	}

	conns, err := connectWebSockets(ctx, endpoint)
	if err != nil {
		return "", nil, err
	}

	var tracker *resolutionTracker
	if targetResolution > 0 {
		tracker = newResolutionTracker(targetResolution, targetCodec, conns.sigConn)
	}

	promise := make(chan wsResult, 1)
	res := &resolver{ch: promise}

	conns.wg.Add(1)
	go func() {
		defer conns.wg.Done()
		handleSignalingMessages(conns.sigConn, res)
	}()

	conns.wg.Add(1)
	go func() {
		defer conns.wg.Done()
		handleMetadataMessages(conns.metaConn, tracker)
	}()

	msg := offerMessage{Type: "offer_sdp", OfferSDP: offer}
	if setErr := conns.sigConn.SetWriteDeadline(time.Now().Add(5 * time.Second)); setErr != nil {
		conns.close()

		return "", nil, setErr
	}
	if writeErr := conns.sigConn.WriteJSON(&msg); writeErr != nil {
		conns.close()

		return "", nil, writeErr
	}

	ans, err := waitForAnswer(ctx, promise, conns)
	if err != nil {
		return "", nil, err
	}

	// Kick off an initil check once we begin receiving metadata.
	triggerInitialTrackCheck(tracker)

	return ans, conns.close, nil
}

// deriveWSURLs returns signaling URL and metadata (.json/.js) URL.
func deriveWSURLs(ep string) (string, string) {
	parsedURL, err := url.Parse(ep)
	if err != nil {
		return ep, ep + ".json"
	}

	// Ceeblue playback: /webrtc/out+<id> <-> /json_out+<id>.js
	if strings.HasPrefix(parsedURL.Path, "/json_out+") && strings.HasSuffix(parsedURL.Path, ".js") {
		id := strings.TrimSuffix(strings.TrimPrefix(parsedURL.Path, "/json_out+"), ".js")
		sig := *parsedURL
		sig.Path = "/webrtc/out+" + id
		sig.RawQuery = ""
		sig.Fragment = ""

		return sig.String(), parsedURL.String()
	}

	if after, ok := strings.CutPrefix(parsedURL.Path, "/webrtc/out+"); ok {
		id := after
		meta := *parsedURL
		meta.Path = "/json_out+" + id + ".js"
		meta.RawQuery = ""
		meta.Fragment = ""

		return parsedURL.String(), meta.String()
	}

	// Default fallback: append .json
	meta := *parsedURL
	meta.Path = parsedURL.Path + ".json"
	meta.RawQuery = ""
	meta.Fragment = ""

	return parsedURL.String(), meta.String()
}

// closeWS gracefully closes a websocket and forces blocking readers to unblock.
func closeWS(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	if err := conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(2*time.Second),
	); err != nil {
		log.Debug().Err(err).Msg("failed to send close control message")
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil { // unblock reads
		log.Debug().Err(err).Msg("failed to set read deadline for close")
	}
	if err := conn.Close(); err != nil {
		log.Debug().Err(err).Msg("failed to close websocket connection")
	}
}

// startKeepAlive configures pong handling and sends periodic ping frames.
func startKeepAlive(ctx context.Context, conn *websocket.Conn) {
	if conn == nil {
		return
	}

	if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		log.Warn().Err(err).Msg("failed to set initial read deadline for keepalive")
	}
	conn.SetPongHandler(func(string) error {
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			log.Warn().Err(err).Msg("failed to set read deadline in pong handler")

			return err
		}

		return nil
	})

	ticker := time.NewTicker(20 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					log.Warn().Err(err).Msg("failed to set write deadline for ping")

					return
				}
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					log.Debug().Err(err).Msg("failed to send ping message")

					return
				}
			}
		}
	}()
}

// extractTracksFromMetadata pulls video track entries from incoming metadata messages.
func extractTracksFromMetadata(data []byte) []resolutionTrack {
	var meta metadataEnvelope
	if err := json.Unmarshal(data, &meta); err != nil {
		log.Warn().Err(err).Str("raw", string(data)).Msg("failed to unmarshal metadata envelope")

		return nil
	}

	tracks := make([]resolutionTrack, 0, len(meta.Meta.Tracks))
	for _, t := range meta.Meta.Tracks {
		if t.Type == "video" {
			tracks = append(tracks, t)
		}
	}

	return tracks
}
