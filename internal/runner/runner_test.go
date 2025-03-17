package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CeeblueTV/webrtc-load-tool/internal/webrtcpeer"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

type testTrack struct {
	rtpPackets uint64
	rtpBytes   uint64
	closed     bool
	kind       webrtc.RTPCodecType
}

func (t *testTrack) MimeType() string {
	return "not implemented"
}

func (t *testTrack) Kind() webrtc.RTPCodecType {
	return t.kind
}

func (t *testTrack) GetPacketsCount() uint64 {
	return t.rtpPackets
}

func (t *testTrack) GetBytesCount() uint64 {
	return t.rtpBytes
}

func (t *testTrack) IsClosed() bool {
	return t.closed
}

type testHandler struct {
	addedConnCh      chan struct{}
	addedConnections *atomic.Uint64
}

func (t *testHandler) AddConnection(_ context.Context) error {
	t.addedConnections.Add(1)

	if t.addedConnCh != nil {
		t.addedConnCh <- struct{}{}
	}

	return nil
}

func TestRunner_callbacks(t *testing.T) {
	closedCalled := &atomic.Uint64{}
	cb := initCallbacks()
	cb.connectionClosedCallback = func() {
		closedCalled.Add(1)
	}

	peer1 := &webrtcpeer.PeerConnection{}
	peer2 := &webrtcpeer.PeerConnection{}

	_, ok := cb.GetConnectionsState(peer1)
	assert.False(t, ok)

	cb.OnNewConnection(peer1)
	state, ok := cb.GetConnectionsState(peer1)
	assert.True(t, ok)
	assert.Equal(t, webrtcpeer.PeerConnectionStatusNew, state)

	assert.Equal(t, int64(1), cb.TotalConnections.Load())
	assert.Equal(t, int64(1), cb.CurrentConnections.Load())

	cb.OnNewConnection(peer1)
	assert.Equal(t, int64(1), cb.TotalConnections.Load())
	assert.Equal(t, int64(1), cb.CurrentConnections.Load())

	// Directly connected!
	cb.OnConnected(peer2)
	state, ok = cb.GetConnectionsState(peer2)
	assert.True(t, ok)
	assert.Equal(t, webrtcpeer.PeerConnectionStatusConnected, state)
	assert.Equal(t, int64(2), cb.TotalConnections.Load())
	assert.Equal(t, int64(2), cb.CurrentConnections.Load())

	cb.OnConnecting(peer1)
	state, ok = cb.GetConnectionsState(peer1)
	assert.True(t, ok)
	assert.Equal(t, webrtcpeer.PeerConnectionStatusConnecting, state)

	cb.OnConnected(peer1)
	state, ok = cb.GetConnectionsState(peer1)
	assert.True(t, ok)
	assert.Equal(t, webrtcpeer.PeerConnectionStatusConnected, state)

	cb.OnDisconnected(peer1)
	state, ok = cb.GetConnectionsState(peer1)
	assert.True(t, ok)
	assert.Equal(t, webrtcpeer.PeerConnectionStatusDisconnected, state)

	cb.OnClosed(peer1)
	_, ok = cb.GetConnectionsState(peer1)
	assert.False(t, ok)
	assert.Equal(t, int64(2), cb.TotalConnections.Load())
	assert.Equal(t, int64(1), cb.CurrentConnections.Load())
	assert.Equal(t, uint64(1), closedCalled.Load())

	cb.OnClosed(peer1)
	assert.Equal(t, int64(2), cb.TotalConnections.Load())
	assert.Equal(t, int64(1), cb.CurrentConnections.Load())
	assert.Equal(t, uint64(1), closedCalled.Load())

	cb.OnClosed(peer2)
	assert.Equal(t, int64(2), cb.TotalConnections.Load())
	assert.Equal(t, int64(0), cb.CurrentConnections.Load())
	assert.Equal(t, uint64(2), closedCalled.Load())
}

func TestRunner_callbacksTracks(t *testing.T) {
	videoTrack1 := &testTrack{
		kind:       webrtc.RTPCodecTypeVideo,
		rtpPackets: 10,
		rtpBytes:   100,
	}
	videoTrack2 := &testTrack{
		kind:       webrtc.RTPCodecTypeVideo,
		rtpPackets: 20,
		rtpBytes:   200,
	}
	audioTrack1 := &testTrack{
		kind:       webrtc.RTPCodecTypeAudio,
		rtpPackets: 30,
		rtpBytes:   300,
	}
	audioTrack2 := &testTrack{
		kind:       webrtc.RTPCodecTypeAudio,
		rtpPackets: 40,
		rtpBytes:   400,
	}
	audioTrack3 := &testTrack{
		kind:       webrtc.RTPCodecTypeAudio,
		rtpPackets: 50,
		rtpBytes:   500,
	}
	cb := initCallbacks()

	cb.OnTrack(videoTrack1)
	cb.OnTrack(videoTrack2)
	cb.OnTrack(audioTrack1)
	cb.OnTrack(audioTrack2)
	cb.OnTrack(audioTrack3)

	expectedVideoPackets := videoTrack1.rtpPackets + videoTrack2.rtpPackets
	assert.Equal(t, int64(2), cb.TotalVideoTracks.Load())
	assert.Equal(t, int64(2), cb.CurrentVideoTracks.Load())
	assert.Equal(t, int64(3), cb.TotalAudioTracks.Load())
	assert.Equal(t, int64(3), cb.CurrentAudioTracks.Load())
	assert.Equal(t, expectedVideoPackets, cb.TotalVideoPackets.Load())
	assert.Equal(t, expectedVideoPackets*10, cb.TotalVideoBytes.Load())
	expectedAudioPackets := audioTrack1.rtpPackets + audioTrack2.rtpPackets + audioTrack3.rtpPackets
	assert.Equal(t, expectedAudioPackets, cb.TotalAudioPackets.Load())
	assert.Equal(t, expectedAudioPackets*10, cb.TotalAudioBytes.Load())

	assert.Equal(t, int64(2), cb.TotalVideoTracks.Load())
	assert.Equal(t, int64(3), cb.TotalAudioTracks.Load())

	stats := cb.GetTracksPacketsDelta()
	assert.Equal(t, expectedAudioPackets, cb.TotalAudioPackets.Load())
	assert.Equal(t, expectedVideoPackets, cb.TotalVideoPackets.Load())
	assert.Equal(t, uint64(0), stats.AudioPacketsDelta)
	assert.Equal(t, uint64(0), stats.VideoBytesDelta)
	assert.Equal(t, uint64(0), stats.VideoPacketsDelta)
	assert.Equal(t, uint64(0), stats.AudioBytesDelta)

	audioTrack1.rtpPackets += 10
	videoTrack1.rtpPackets += 10
	audioTrack2.rtpPackets += 13
	videoTrack2.rtpPackets += 7
	audioTrack1.rtpBytes += 10 * 10
	videoTrack1.rtpBytes += 10 * 10
	audioTrack2.rtpBytes += 13 * 10
	videoTrack2.rtpBytes += 7 * 10
	expectedAudioPackets += 10 + 13
	expectedVideoPackets += 10 + 7

	stats = cb.GetTracksPacketsDelta()
	assert.Equal(t, expectedAudioPackets, cb.TotalAudioPackets.Load())
	assert.Equal(t, expectedAudioPackets*10, cb.TotalAudioBytes.Load())
	assert.Equal(t, expectedVideoPackets, cb.TotalVideoPackets.Load())
	assert.Equal(t, expectedVideoPackets*10, cb.TotalVideoBytes.Load())
	assert.Equal(t, uint64(23), stats.AudioPacketsDelta)
	assert.Equal(t, uint64(23*10), stats.AudioBytesDelta)
	assert.Equal(t, uint64(17), stats.VideoPacketsDelta)
	assert.Equal(t, uint64(17*10), stats.VideoBytesDelta)

	audioTrack3.rtpPackets += 20
	audioTrack3.closed = true
	// should count packets for the last time before removing the track.
	expectedAudioPackets += 20
	stats = cb.GetTracksPacketsDelta()
	assert.Equal(t, int64(3), cb.TotalAudioTracks.Load())
	assert.Equal(t, int64(2), cb.CurrentAudioTracks.Load())
	assert.Equal(t, expectedAudioPackets, cb.TotalAudioPackets.Load())
	assert.Equal(t, expectedVideoPackets, cb.TotalVideoPackets.Load())
	assert.Equal(t, uint64(20), stats.AudioPacketsDelta)
	assert.Equal(t, uint64(0), stats.VideoPacketsDelta)
	audioTrack3.rtpPackets += 10
	stats = cb.GetTracksPacketsDelta()
	assert.Equal(t, expectedAudioPackets, cb.TotalAudioPackets.Load())
	assert.Equal(t, expectedVideoPackets, cb.TotalVideoPackets.Load())
	assert.Equal(t, uint64(0), stats.AudioPacketsDelta)
	assert.Equal(t, uint64(0), stats.VideoPacketsDelta)
}

func TestRunner_New(t *testing.T) {
	r, err := New(Config{
		WhipEndpoint: "http://ceeblue.net",
	})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestConnectionsStats(t *testing.T) {
	stats := &connectionsStats{}
	assert.Equal(t, "", stats.String())

	stats = &connectionsStats{
		webrtcpeer.PeerConnectionStatusNew:        1,
		webrtcpeer.PeerConnectionStatusConnecting: 2,
	}
	assert.Equal(t, "New: 1, Connecting: 2", stats.String())

	stats = &connectionsStats{
		webrtcpeer.PeerConnectionStatusNew: 1,
	}
	assert.Equal(t, "New: 1", stats.String())

	stats = &connectionsStats{
		webrtcpeer.PeerConnectionStatusNew:          1,
		webrtcpeer.PeerConnectionStatusConnecting:   2,
		webrtcpeer.PeerConnectionStatusConnected:    3,
		webrtcpeer.PeerConnectionStatusDisconnected: 4,
	}
	assert.Equal(t, "New: 1, Connecting: 2, Connected: 3, Disconnected: 4", stats.String())
}

func TestCountConnections(t *testing.T) {
	cb := initCallbacks()

	connections := cb.CountConnections()
	assert.Empty(t, connections.String())

	cb = initCallbacks()
	cb.OnConnected(&webrtcpeer.PeerConnection{})
	connections = cb.CountConnections()
	assert.Equal(t, 1, connections[webrtcpeer.PeerConnectionStatusConnected])

	cb = initCallbacks()
	peer := &webrtcpeer.PeerConnection{}
	cb.OnNewConnection(peer)
	cb.OnConnected(peer)
	connections = cb.CountConnections()
	assert.Equal(t, 1, connections[webrtcpeer.PeerConnectionStatusConnected])
}

func TestRunner_runner(t *testing.T) {
	t.Run("Runs connections with if no runup", func(t *testing.T) {
		handler := &testHandler{
			addedConnections: &atomic.Uint64{},
		}
		runs := uint(10)
		runner := &runnerImpl{
			handler: handler,
			config: Config{
				Connections: runs,
				Duration:    time.Millisecond * 500,
			},
		}

		assert.True(t, runner.Run(context.Background()), "Expected runner to finish")
		time.Sleep(time.Millisecond * 200)
		assert.Equal(t, uint64(runs), handler.addedConnections.Load())
	})

	t.Run("Try to reconnect if connection close", func(t *testing.T) {
		ch := make(chan struct{})
		handler := &testHandler{
			addedConnCh:      ch,
			addedConnections: &atomic.Uint64{},
		}
		runs := uint(10)
		runner := &runnerImpl{
			handler: handler,
			config: Config{
				Connections: runs,
				Duration:    time.Millisecond * 500,
			},
		}
		go func() {
			closed := uint(0)
			for range ch {
				if closed >= runs {
					continue
				}

				closed++
				runner.connectionClosedCallback()
			}
		}()

		assert.True(t, runner.Run(context.Background()), "Expected runner to finish")
		time.Sleep(time.Millisecond * 200)
		assert.GreaterOrEqual(t, handler.addedConnections.Load()*2, uint64(runs))
	})

	t.Run("exit while reconnecting", func(t *testing.T) {
		ch := make(chan struct{})
		handler := &testHandler{
			addedConnCh:      ch,
			addedConnections: &atomic.Uint64{},
		}
		runs := uint(100)
		runner := &runnerImpl{
			handler: handler,
			config: Config{
				Connections: runs,
				Duration:    time.Millisecond * 500,
			},
		}
		go func() {
			for range ch {
				runner.connectionClosedCallback()
			}
		}()

		assert.True(t, runner.Run(context.Background()), "Expected runner to finish")
	})

	t.Run("Band input", func(t *testing.T) {
		handler := &testHandler{
			addedConnections: &atomic.Uint64{},
		}
		runner := &runnerImpl{
			handler: handler,
			config: Config{
				Connections: 0,
				Duration:    time.Millisecond * 200,
				Runup:       time.Millisecond * 10,
			},
		}
		// making sure that calling connectionClosedCallback is safe before the run
		runner.connectionClosedCallback()
		assert.True(t, runner.Run(context.Background()), "Expected runner to finish")
		assert.Equal(t, uint64(0), handler.addedConnections.Load())
	})

	t.Run("Runup", func(t *testing.T) {
		ch := make(chan struct{})
		handler := &testHandler{
			addedConnCh:      ch,
			addedConnections: &atomic.Uint64{},
		}
		runs := uint(3)
		runner := &runnerImpl{
			handler: handler,
			config: Config{
				Connections: runs,
				Duration:    time.Millisecond * 1000,
				Runup:       time.Millisecond * 300, // 100ms per connection
			},
		}
		go func() {
			closed := uint(0)
			for range ch {
				if closed >= runs {
					continue
				}

				closed++
				runner.connectionClosedCallback()
			}
		}()

		startTime := time.Now()
		assert.True(t, runner.Run(context.Background()))
		duration := time.Since(startTime)

		// we reconnect all the connections once!
		expectedSpacing := time.Millisecond * 100 * time.Duration(runs*2) //nolint:gosec // G115
		assert.GreaterOrEqual(t, duration, expectedSpacing, "Connections should be spaced out by runup timing")
		assert.Equal(t, uint64(runs*2), handler.addedConnections.Load())
	})
}
