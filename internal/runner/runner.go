// Package runner is responsible for running the load test
// And aggregating and logging the results.
package runner

import (
	"context"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CeeblueTV/webrtc-load-tool/internal/webrtcpeer"
	"github.com/dustin/go-humanize"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

// Runner is the main interface of the load test runner.
type Runner interface {
	// Run a test run, This is a blocking call.
	// For the same runner, Run can be used once at a time.
	// Use Stop to stop the test run.
	// Returns true if the test run was run until the end, false if it was stopped.
	// Returns an error if the test run failed.
	Run(ctx context.Context) (ok bool)
}

// Config is the configuration of the load test runner.
type Config struct {
	// WhipEndpoint is the URL of the WHIP server.
	WhipEndpoint string
	// ICEServers is the list of ICE servers to use.
	ICEServers []webrtc.ICEServer
	// ICETransport policy to use.
	ICETransportPolicy webrtc.ICETransportPolicy
	// Connections is the maximum number of connections to create.
	Connections uint
	// Runup is the time frame to create maximum number of connections.
	// wait between each connection creation = Runup / Connections
	Runup time.Duration
	// Duration is the time to run the test.
	Duration time.Duration
	// LiteMode If enabled no Video or Audio handling.
	LiteMode bool
	// BufferDuration is the buffer duration for RTP jitter buffer for lost packets counter.
	BufferDuration time.Duration
}

type callbacks struct {
	connectionClosedCallback                     func()
	TotalConnections, CurrentConnections         *atomic.Int64
	TotalVideoTracks, CurrentVideoTracks         *atomic.Int64
	TotalAudioTracks, CurrentAudioTracks         *atomic.Int64
	TotalAudioPackets, TotalVideoPackets         *atomic.Uint64
	TotalAudioBytes, TotalVideoBytes             *atomic.Uint64
	TotalLostAudioPackets, TotalLostVideoPackets *atomic.Uint64

	// [*webrtcpeer.PeerConnection] -> webrtcpeer.PeerConnectionStatus
	connectionsState *sync.Map
	// [webrtcpeer.TrackInfo] -> [last_bytes_count uint64, last_packets_count uint64, last_lost_packets_count uint64]
	tracks *sync.Map
}

func (t *callbacks) OnNewConnection(peer *webrtcpeer.PeerConnection) {
	t.swapPeerState(peer, webrtcpeer.PeerConnectionStatusNew)
}

func (t *callbacks) OnConnecting(peer *webrtcpeer.PeerConnection) {
	t.swapPeerState(peer, webrtcpeer.PeerConnectionStatusConnecting)
}

func (t *callbacks) OnConnected(peer *webrtcpeer.PeerConnection) {
	t.swapPeerState(peer, webrtcpeer.PeerConnectionStatusConnected)
}

func (t *callbacks) OnDisconnected(peer *webrtcpeer.PeerConnection) {
	t.swapPeerState(peer, webrtcpeer.PeerConnectionStatusDisconnected)
}

func (t *callbacks) swapPeerState(peer *webrtcpeer.PeerConnection, state webrtcpeer.PeerConnectionStatus) {
	_, ok := t.connectionsState.Swap(peer, state)

	// Only count new connections!
	if !ok {
		t.TotalConnections.Add(1)
		t.CurrentConnections.Add(1)
	}
}

func (t *callbacks) OnClosed(peer *webrtcpeer.PeerConnection) {
	if _, loaded := t.connectionsState.LoadAndDelete(peer); !loaded {
		// protection against double close from pion side!
		return
	}

	t.CurrentConnections.Add(-1)
	t.connectionClosedCallback()
}

func (t *callbacks) GetConnectionsState(peer *webrtcpeer.PeerConnection) (
	state webrtcpeer.PeerConnectionStatus,
	ok bool,
) {
	value, ok := t.connectionsState.Load(peer)
	if !ok {
		return 0, false
	}

	val, ok := value.(webrtcpeer.PeerConnectionStatus)

	return val, ok
}

func (t *callbacks) OnTrack(track webrtcpeer.TrackInfo) {
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		t.TotalAudioTracks.Add(1)
		t.CurrentAudioTracks.Add(1)
		t.TotalAudioPackets.Add(track.GetPacketsCount())
		t.TotalAudioBytes.Add(track.GetBytesCount())
		t.TotalLostAudioPackets.Add(track.GetLostPacketsCount())
	} else {
		t.TotalVideoTracks.Add(1)
		t.CurrentVideoTracks.Add(1)
		t.TotalVideoPackets.Add(track.GetPacketsCount())
		t.TotalVideoBytes.Add(track.GetBytesCount())
		t.TotalLostVideoPackets.Add(track.GetLostPacketsCount())
	}

	t.tracks.Store(track, []uint64{track.GetBytesCount(), track.GetPacketsCount(), track.GetLostPacketsCount()})
}

type tracksPacketsDelta struct {
	VideoPacketsDelta, AudioPacketsDelta         uint64
	VideoLostPacketsDelta, AudioLostPacketsDelta uint64
	VideoBytesDelta, AudioBytesDelta             uint64
}

// GetTracksPacketsDelta returns the stats of the tracks packets.
// The stats are calculated from the last call.
// Removes closed tracks from the stats.
func (t *callbacks) GetTracksPacketsDelta() tracksPacketsDelta {
	var videoPacketsDelta, audioPacketsDelta uint64
	var videoBytesDelta, audioBytesDelta uint64
	var videoLostPacketsDelta, audioLostPacketsDelta uint64
	t.tracks.Range(func(key, value any) bool {
		track, ok := key.(webrtcpeer.TrackInfo)
		if !ok {
			return true
		}

		last, ok := value.([]uint64)
		if !ok || len(last) != 3 {
			return true
		}
		lastBytesCount, lastPacketCount, lastLostPacketsCount := last[0], last[1], last[2]
		packetsCount := track.GetPacketsCount()
		lostPacketsCount := track.GetLostPacketsCount()
		bytesCount := track.GetBytesCount()

		if track.Kind() == webrtc.RTPCodecTypeAudio {
			audioPacketsDelta += packetsCount - lastPacketCount
			audioBytesDelta += bytesCount - lastBytesCount
			audioLostPacketsDelta += lostPacketsCount - lastLostPacketsCount
		} else {
			videoPacketsDelta += packetsCount - lastPacketCount
			videoBytesDelta += bytesCount - lastBytesCount
			videoLostPacketsDelta += lostPacketsCount - lastLostPacketsCount
		}

		if track.IsClosed() {
			t.tracks.Delete(track)
			if track.Kind() == webrtc.RTPCodecTypeAudio {
				t.CurrentAudioTracks.Add(-1)
			} else {
				t.CurrentVideoTracks.Add(-1)
			}

			return true
		}

		t.tracks.Store(track, []uint64{bytesCount, packetsCount, lostPacketsCount})

		return true
	})

	t.TotalVideoPackets.Add(videoPacketsDelta)
	t.TotalAudioPackets.Add(audioPacketsDelta)
	t.TotalVideoBytes.Add(videoBytesDelta)
	t.TotalAudioBytes.Add(audioBytesDelta)
	t.TotalLostVideoPackets.Add(videoLostPacketsDelta)
	t.TotalLostAudioPackets.Add(audioLostPacketsDelta)

	return tracksPacketsDelta{
		VideoPacketsDelta:     videoPacketsDelta,
		AudioPacketsDelta:     audioPacketsDelta,
		VideoBytesDelta:       videoBytesDelta,
		AudioBytesDelta:       audioBytesDelta,
		VideoLostPacketsDelta: videoLostPacketsDelta,
		AudioLostPacketsDelta: audioLostPacketsDelta,
	}
}

type connectionsStats map[webrtcpeer.PeerConnectionStatus]int

func (t connectionsStats) String() string {
	if len(t) == 0 {
		return ""
	}

	order := []webrtcpeer.PeerConnectionStatus{
		webrtcpeer.PeerConnectionStatusNew,
		webrtcpeer.PeerConnectionStatusConnecting,
		webrtcpeer.PeerConnectionStatusConnected,
		webrtcpeer.PeerConnectionStatusDisconnected,
	}

	str := ""
	for _, state := range order {
		count := t[state]
		if count == 0 {
			continue
		}

		if str != "" {
			str += ", "
		}
		statestr := state.String()
		str += strings.ToUpper(statestr[:1]) + statestr[1:] + ": " + humanize.Comma(int64(count))
	}

	return str
}

func (t *callbacks) CountConnections() connectionsStats {
	counts := connectionsStats{
		webrtcpeer.PeerConnectionStatusNew:          0,
		webrtcpeer.PeerConnectionStatusConnecting:   0,
		webrtcpeer.PeerConnectionStatusConnected:    0,
		webrtcpeer.PeerConnectionStatusDisconnected: 0,
	}

	t.connectionsState.Range(func(_, value any) bool {
		state, ok := value.(webrtcpeer.PeerConnectionStatus)
		if !ok {
			return true
		}

		counts[state]++

		return true
	})

	return counts
}

func initCallbacks() *callbacks {
	return &callbacks{
		connectionClosedCallback: func() {},
		TotalConnections:         &atomic.Int64{},
		CurrentConnections:       &atomic.Int64{},
		TotalVideoTracks:         &atomic.Int64{},
		CurrentVideoTracks:       &atomic.Int64{},
		TotalAudioTracks:         &atomic.Int64{},
		CurrentAudioTracks:       &atomic.Int64{},
		TotalAudioPackets:        &atomic.Uint64{},
		TotalVideoPackets:        &atomic.Uint64{},
		TotalVideoBytes:          &atomic.Uint64{},
		TotalAudioBytes:          &atomic.Uint64{},
		TotalLostAudioPackets:    &atomic.Uint64{},
		TotalLostVideoPackets:    &atomic.Uint64{},
		connectionsState:         &sync.Map{},
		tracks:                   &sync.Map{},
	}
}

// New creates a new WebRTC load test runner.
func New(config Config) (Runner, error) {
	cb := initCallbacks()
	handle, err := newWebRTCHandler(webrtcpeer.Configuration{
		ICEServers:         config.ICEServers,
		ICETransportPolicy: config.ICETransportPolicy,
		LiteMode:           config.LiteMode,
		BufferDuration:     config.BufferDuration,
	}, config.WhipEndpoint, cb)
	if err != nil {
		return nil, err
	}

	runner := &runnerImpl{
		config:    config,
		callbacks: cb,
		handler:   handle,
	}
	cb.connectionClosedCallback = runner.connectionClosedCallback

	return runner, nil
}

type runnerImpl struct {
	handler   handler
	callbacks *callbacks
	sem       *semaphore.Weighted
	config    Config
}

func (r *runnerImpl) Run(ctx context.Context) (ok bool) {
	if r.config.Connections == 0 {
		return true
	}
	log.Info().Msg("Starting test run")

	waitForLogs := make(chan struct{}, 1)
	runnerCtx, cancel := context.WithCancel(ctx)
	finished := atomic.Bool{}
	//nolint:gosec // G115 no overflow, false positive
	r.sem = semaphore.NewWeighted(int64(r.config.Connections))
	go func() {
		time.Sleep(r.config.Duration)
		finished.Store(true)
		cancel()
	}()
	go func() {
		r.logger(runnerCtx)

		waitForLogs <- struct{}{}
	}()

	grace := time.Duration(1)
	if r.config.Runup > 0 {
		grace = r.config.Runup / time.Duration(r.config.Connections) //nolint:gosec // G115
	}
	ticker := time.NewTicker(grace)

	for runnerCtx.Err() == nil {
		err := r.sem.Acquire(runnerCtx, 1)
		if err != nil {
			break
		}

		go func() {
			err := r.handler.AddConnection(runnerCtx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to start connection")
			}
		}()

		if r.config.Runup > 0 {
			<-ticker.C
		}
	}

	<-waitForLogs

	return finished.Load()
}

func (r *runnerImpl) connectionClosedCallback() {
	// Runner didn't start yet!
	if r.sem == nil {
		return
	}

	r.sem.Release(1)
}

func (r *runnerImpl) logger(ctx context.Context) {
	// Runner wasn't initialized correctly! (test)
	if r.callbacks == nil {
		return
	}

	tick := time.NewTicker(time.Second)

	for {
		select {
		case <-ctx.Done():
			r.sumLog()

			return
		case <-tick.C:
			r.logChange()
		}
	}
}

func (r *runnerImpl) logChange() {
	stats := r.callbacks.GetTracksPacketsDelta()
	videoTracks := r.callbacks.CurrentVideoTracks.Load()
	audioTracks := r.callbacks.CurrentAudioTracks.Load()

	msg := log.Info().
		Str("Connections", r.callbacks.CountConnections().String())

	if videoTracks > 0 {
		msg = msg.
			Str("Video_Tracks", humanize.Comma(videoTracks))
	}

	if audioTracks > 0 {
		msg = msg.
			Str("Audio_Tracks", humanize.Comma(audioTracks))
	}

	if stats.AudioBytesDelta > 0 {
		msg = msg.
			Str("Audio_Bandwidth", formatBits(stats.AudioBytesDelta)+"s")

		if audioTracks > 1 {
			msg = msg.
				Str("Audio_Average", formatBits(stats.AudioBytesDelta/uint64(audioTracks))+"s")
		}
	}

	if stats.VideoBytesDelta > 0 {
		msg = msg.
			Str("Video_Bandwidth", formatBits(stats.VideoBytesDelta)+"s")

		if videoTracks > 1 {
			msg = msg.
				Str("Video_Average", formatBits(stats.VideoBytesDelta/uint64(videoTracks))+"s")
		}
	}

	if stats.AudioLostPacketsDelta > 0 {
		msg = msg.
			Str("Audio_Lost_Packets", humanize.Comma(int64(stats.AudioLostPacketsDelta))) // nolint:gosec // G115
	}

	if stats.VideoLostPacketsDelta > 0 {
		msg = msg.
			Str("Video_Lost_Packets", humanize.Comma(int64(stats.VideoLostPacketsDelta))) // nolint:gosec // G115
	}

	msg.Msg("Stats")
}

func (r *runnerImpl) sumLog() {
	log.Info().
		Str("Total_Connections", humanize.Comma(r.callbacks.TotalConnections.Load())).
		Str("Total_Video_Tracks", humanize.Comma(r.callbacks.TotalVideoTracks.Load())).
		Str("Total_Audio_Tracks", humanize.Comma(r.callbacks.TotalAudioTracks.Load())).
		//nolint:gosec // G115
		Str("Total_Video_Packets", humanize.Comma(int64(r.callbacks.TotalVideoPackets.Load()))).
		//nolint:gosec // G115
		Str("Total_Audio_Packets", humanize.Comma(int64(r.callbacks.TotalAudioPackets.Load()))).
		Str("Total_Video_Bytes", humanize.Bytes(r.callbacks.TotalVideoBytes.Load())).
		Str("Total_Audio_Bytes", humanize.Bytes(r.callbacks.TotalAudioBytes.Load())).
		//nolint:gosec // G115
		Str("Total_Lost_Video_Packets", humanize.Comma(int64(r.callbacks.TotalLostVideoPackets.Load()))).
		//nolint:gosec // G115
		Str("Total_Lost_Audio_Packets", humanize.Comma(int64(r.callbacks.TotalLostAudioPackets.Load()))).
		Str("Connections", r.callbacks.CountConnections().String()).
		Msg("Test run finished")
}

func formatBits(bits uint64) string {
	bytes := float64(bits) * 8

	formatted := humanize.Bytes(uint64(math.Ceil(bytes)))
	formatted = strings.Replace(formatted, "B", "bit", 1)

	return formatted
}
