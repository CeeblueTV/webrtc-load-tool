package webrtc

import (
	"sync"
	"sync/atomic"

	"github.com/pion/webrtc/v4"
)

// PeerConnectionStatus represents the status of a PeerConnection.
type PeerConnectionStatus uint8

const (
	// PeerConnectionStatusNew indicates that the PeerConnection is new
	// Waiting for the SDP answer.
	PeerConnectionStatusNew PeerConnectionStatus = iota
	// PeerConnectionStatusConnecting indicates that the PeerConnection is connecting.
	PeerConnectionStatusConnecting
	// PeerConnectionStatusConnected indicates that the PeerConnection is connected.
	PeerConnectionStatusConnected
	// PeerConnectionStatusDisconnected indicates that the PeerConnection is disconnected.
	PeerConnectionStatusDisconnected
	// PeerConnectionStatusClosed indicates that the PeerConnection is closed.
	// This is the final state of the PeerConnection.
	PeerConnectionStatusClosed
)

func (s PeerConnectionStatus) String() string {
	switch s {
	case PeerConnectionStatusNew:
		return "new"
	case PeerConnectionStatusConnecting:
		return "connecting"
	case PeerConnectionStatusConnected:
		return "connected"
	case PeerConnectionStatusDisconnected:
		return "disconnected"
	case PeerConnectionStatusClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// VideoCodec is the video codec to use in the PeerConnection.
type VideoCodec uint8

const (
	// VideoCodecAuto Allow any video codec added by Pion default media engine.
	VideoCodecAuto VideoCodec = iota
	// VideoCodecVP8 Use VP8 video codec.
	VideoCodecVP8
	// VideoCodecH264 Use H264 video codec.
	VideoCodecH264
)

// Configuration represents the configuration of a PeerConnection.
type Configuration struct {
	iceServers []webrtc.ICEServer
	VideoCodec VideoCodec
}

// API represents the WebRTC API for creating PeerConnections
// Based on the Pion WebRTC API and WHIP signaling.
type API struct {
	api        *webrtc.API
	iceServers []webrtc.ICEServer
	videoCodec VideoCodec
}

func newAPI(config Configuration) (*API, error) {
	mediaEngine, err := buildMediaEngine(config.VideoCodec)
	if err != nil {
		return nil, err
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	return &API{
		api:        api,
		iceServers: config.iceServers,
		videoCodec: config.VideoCodec,
	}, nil
}

func buildMediaEngine(codec VideoCodec) (mediaEngine *webrtc.MediaEngine, err error) {
	mediaEngine = &webrtc.MediaEngine{}
	addOPUS := true

	switch codec {
	case VideoCodecVP8:
		err = mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     webrtc.MimeTypeVP8,
				ClockRate:    90000,
				Channels:     0,
				SDPFmtpLine:  "",
				RTCPFeedback: nil,
			},
			PayloadType: 96,
		}, webrtc.RTPCodecTypeVideo)
	case VideoCodecH264:
		err = mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     webrtc.MimeTypeH264,
				ClockRate:    90000,
				Channels:     0,
				SDPFmtpLine:  "",
				RTCPFeedback: nil,
			},
			PayloadType: 102,
		}, webrtc.RTPCodecTypeVideo)
	default:
		addOPUS = false
		err = mediaEngine.RegisterDefaultCodecs()
	}

	if err != nil {
		return nil, err
	}

	if addOPUS {
		err = mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     webrtc.MimeTypeOpus,
				ClockRate:    48000,
				Channels:     2,
				SDPFmtpLine:  "",
				RTCPFeedback: nil,
			},
			PayloadType: 111,
		}, webrtc.RTPCodecTypeAudio)
		if err != nil {
			return nil, err
		}
	}

	return mediaEngine, nil
}

// ChangeCallback is a callback function that is called when the PeerConnection changes.
type ChangeCallback func(change ChangeCallBackType)

// ChangeCallBackType represents the type of change in the PeerConnection.
type ChangeCallBackType int

const (
	// ChangeCallBackTypeStateChagne indicates that the state of the PeerConnection has changed.
	ChangeCallBackTypeStateChagne ChangeCallBackType = iota
	// ChangeCallBackTypeNewTrack indicates that a new track has been added to the PeerConnection.
	ChangeCallBackTypeNewTrack
)

// NewPeerConnection creates a new PeerConnection with video and audio
// transceivers, wait for the gather of ICE candidates and return the
// PeerConnection.
func (a *API) NewPeerConnection(callback ChangeCallback) (*PeerConnection, error) {
	pc, err := a.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: a.iceServers,
	})
	if err != nil {
		return nil, err
	}

	peer := &PeerConnection{
		pc:       pc,
		state:    new(atomic.Int32),
		callback: callback,
		mu:       new(sync.RWMutex),
	}

	pc.OnConnectionStateChange(peer.handleConnectionStateChange)
	pc.OnTrack(peer.handleTrack)

	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return nil, err
	}

	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return nil, err
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, err
	}

	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, err
	}

	return peer, nil
}

// PeerConnection represents a basic WebRTC peer connection.
// For simulation purposes, it holds the state of the connection.
// And how many RTP packets were received.
type PeerConnection struct {
	pc       *webrtc.PeerConnection
	state    *atomic.Int32
	callback ChangeCallback
	mu       *sync.RWMutex
	tracks   []*TrackInfo
}

// GetStatus returns the current status of the PeerConnection.
func (p *PeerConnection) GetStatus() PeerConnectionStatus {
	return PeerConnectionStatus(p.state.Load()) // nolint:gosec // G115 PeerConnectionStatus is a small uint8 enum
}

func (p *PeerConnection) setStatus(status PeerConnectionStatus) {
	p.state.Store(int32(status)) //nolint:gosec // G115 PeerConnectionStatus is a small uint8 enum
}

// GetOffer returns the local SDP of the PeerConnection
// ALways is offer and includes the ICE candidates.
func (p *PeerConnection) GetOffer() string {
	return p.pc.LocalDescription().SDP
}

// SetAnswer sets the remote SDP of the PeerConnection
// Should be the answer and includes the ICE candidates (if required / any).
func (p *PeerConnection) SetAnswer(answer string) error {
	return p.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	})
}

func (p *PeerConnection) handleConnectionStateChange(state webrtc.PeerConnectionState) {
	switch state {
	case webrtc.PeerConnectionStateNew,
		webrtc.PeerConnectionStateConnecting,
		webrtc.PeerConnectionStateUnknown:
		p.setStatus(PeerConnectionStatusNew)
	case webrtc.PeerConnectionStateConnected:
		p.setStatus(PeerConnectionStatusConnected)
	case webrtc.PeerConnectionStateDisconnected:
		p.setStatus(PeerConnectionStatusDisconnected)
	case webrtc.PeerConnectionStateClosed,
		webrtc.PeerConnectionStateFailed:
		p.setStatus(PeerConnectionStatusClosed)
	}

	p.callback(ChangeCallBackTypeStateChagne)
}

func (p *PeerConnection) handleTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	info := newTrackInfo(track)
	p.mu.Lock()
	p.tracks = append(p.tracks, info)
	p.mu.Unlock()

	p.callback(ChangeCallBackTypeNewTrack)
}

// TrackInfo represents the information of a track in a PeerConnection.
type TrackInfo struct {
	trackRemote *webrtc.TrackRemote
	rtpPackets  *atomic.Uint64
	closed      *atomic.Bool
	MimeType    string
	Kind        webrtc.RTPCodecType
}

func newTrackInfo(track *webrtc.TrackRemote) *TrackInfo {
	info := &TrackInfo{
		MimeType:    track.Codec().MimeType,
		Kind:        track.Kind(),
		trackRemote: track,
		rtpPackets:  new(atomic.Uint64),
		closed:      new(atomic.Bool),
	}

	go func() {
		for {
			_, _, err := track.ReadRTP()
			if err != nil {
				info.setClosed()

				return
			}

			info.increasePacketCount()
		}
	}()

	return info
}

func (t *TrackInfo) increasePacketCount() {
	t.rtpPackets.Add(1)
}

// GetPacketsCount returns the number of RTP packets received by the track during its lifetime.
func (t *TrackInfo) GetPacketsCount() uint64 {
	return t.rtpPackets.Load()
}

// GetTracks returns the info objects of the tracks in the PeerConnection.
func (p *PeerConnection) GetTracks() []*TrackInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	copyTracks := make([]*TrackInfo, len(p.tracks))
	copy(copyTracks, p.tracks)

	return copyTracks
}

func (t *TrackInfo) setClosed() {
	t.closed.Store(true)
}

// IsClosed returns whether the track is closed.
func (t *TrackInfo) IsClosed() bool {
	return t.closed.Load()
}
