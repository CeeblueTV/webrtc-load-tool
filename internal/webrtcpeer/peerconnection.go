package webrtcpeer

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

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
	ICEServers         []webrtc.ICEServer
	ICETransportPolicy webrtc.ICETransportPolicy
	LiteMode           bool
	VideoCodec         VideoCodec
	BufferSize         time.Duration
}

// Client represents the WebRTC Client for creating PeerConnections
// Based on the Pion WebRTC API and WHIP signaling.
type Client struct {
	api        *webrtc.API
	httpClient *http.Client
	callback   ChangeCallback
	iceServers []webrtc.ICEServer
	icePolicy  webrtc.ICETransportPolicy
	liteMode   bool
	videoCodec VideoCodec
	bufferSize time.Duration
}

// NewClient creates a new WebRTC Client with the given configuration.
// It handles the creation of PeerConnections and the signaling over WHIP.
func NewClient(config Configuration, callback ChangeCallback) (*Client, error) {
	mediaEngine, err := buildMediaEngine(config.VideoCodec)
	if err != nil {
		return nil, err
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	return &Client{
		api:        api,
		iceServers: config.ICEServers,
		icePolicy:  config.ICETransportPolicy,
		liteMode:   config.LiteMode,
		videoCodec: config.VideoCodec,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		callback:   callback,
		bufferSize: config.BufferSize,
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

// NewPeerConnection creates a new PeerConnection and handles WHIP signaling.
func (a *Client) NewPeerConnection(ctx context.Context, whipEndpoint string) (*PeerConnection, error) {
	peer, err := a.initPeerConnection()
	if err != nil {
		return nil, err
	}

	answer, err := whip(ctx, a.httpClient, whipEndpoint, peer.GetOffer())
	if err != nil {
		closeErr := peer.pc.Close()
		if closeErr != nil {
			return nil, err
		}

		return nil, err
	}

	if err := peer.SetAnswer(answer); err != nil {
		return nil, err
	}

	return peer, nil
}

// ChangeCallback is a callback function that is called when the PeerConnection changes.
type ChangeCallback func(peer *PeerConnection, change ChangeCallBackType, data any)

// ChangeCallBackType represents the type of change in the PeerConnection.
type ChangeCallBackType int

const (
	// ChangeCallBackTypeStateChagne indicates that the state of the PeerConnection has changed.
	ChangeCallBackTypeStateChagne ChangeCallBackType = iota
	// ChangeCallBackTypeNewTrack indicates that a new track has been added to the PeerConnection.
	ChangeCallBackTypeNewTrack
)

// initPeerConnection creates a new PeerConnection with video and audio
// transceivers, wait for the gather of ICE candidates and return the
// PeerConnection.
func (a *Client) initPeerConnection() (*PeerConnection, error) {
	pc, err := a.api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         a.iceServers,
		ICETransportPolicy: a.icePolicy,
	})
	if err != nil {
		return nil, err
	}

	peer := &PeerConnection{
		pc:         pc,
		state:      new(atomic.Int32),
		callback:   a.callback,
		mu:         new(sync.RWMutex),
		bufferSize: a.bufferSize,
	}

	pc.OnConnectionStateChange(peer.handleConnectionStateChange)
	if !a.liteMode {
		pc.OnTrack(peer.handleTrack)
	}

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

	go peer.handleConnectionStateChange(webrtc.PeerConnectionStateNew)

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
	pc         *webrtc.PeerConnection
	state      *atomic.Int32
	callback   ChangeCallback
	mu         *sync.RWMutex
	tracks     []TrackInfo
	bufferSize time.Duration
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
	var status PeerConnectionStatus
	switch state {
	case webrtc.PeerConnectionStateNew:
		status = PeerConnectionStatusNew
	case webrtc.PeerConnectionStateConnecting,
		webrtc.PeerConnectionStateUnknown:
		status = PeerConnectionStatusConnecting
	case webrtc.PeerConnectionStateConnected:
		status = PeerConnectionStatusConnected
	case webrtc.PeerConnectionStateDisconnected:
		status = PeerConnectionStatusDisconnected
	case webrtc.PeerConnectionStateClosed,
		webrtc.PeerConnectionStateFailed:
		status = PeerConnectionStatusClosed
		p.closeAllTracks()
	}

	p.setStatus(status)
	p.callback(p, ChangeCallBackTypeStateChagne, status)
}

func (p *PeerConnection) closeAllTracks() {
	p.mu.Lock()
	for _, track := range p.tracks {
		t, ok := track.(*trackInfoImpl)
		if ok {
			t.setClosed()
		}
	}
	p.mu.Unlock()
}

// Close closes the underlying PeerConnection.
func (p *PeerConnection) Close() error {
	return p.pc.Close()
}

func (p *PeerConnection) handleTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	info := newTrackInfo(track, p.bufferSize)
	p.mu.Lock()
	p.tracks = append(p.tracks, info)
	p.mu.Unlock()

	p.callback(p, ChangeCallBackTypeNewTrack, info)
}

// TrackInfo represents the information of a media track in a PeerConnection.
type TrackInfo interface {
	// MimeType returns the MIME type of the track.
	MimeType() string
	// Kind returns the kind of the track.
	Kind() webrtc.RTPCodecType
	// GetPacketsCount returns the number of RTP packets received by the track during its lifetime.
	GetPacketsCount() uint64
	// GetLostPacketsCount returns the number of RTP packets lost by the track during its lifetime.
	GetLostPacketsCount() uint64
	// GetBytesCount returns the number of bytes received by the track during its lifetime.
	GetBytesCount() uint64
	// IsClosed returns whether the track is closed.
	IsClosed() bool
}

// A simple RTP packet jitter and loss tracker.
type lostPacketsTracker struct {
	packets    []int
	timestamps []uint32
	lastSeq    int
	// BufferSize in milliseconds.
	BufferSize uint32
	started    bool
}

// ReportPacket adds a packet to the tracker and reports any lost packets.
func (t *lostPacketsTracker) ReportPacket(sequence uint16, timestamp uint32) uint {
	t.addPacket(sequence, timestamp)

	cutOff := timestamp - t.BufferSize
	var dropped uint
	for len(t.timestamps) > 0 && t.timestamps[0] < cutOff {
		seq := t.packets[0]

		if !t.started {
			t.started = true
			t.lastSeq = seq
		} else {
			if seq > t.lastSeq+1 {
				//nolint:gosec // G115 no overflows possible
				dropped += uint(seq - t.lastSeq - 1)
			}
			t.lastSeq = seq
		}

		t.packets = t.packets[1:]
		t.timestamps = t.timestamps[1:]
	}

	return dropped
}

// AddPacket efficiently add a packet to the tracker (sorted slice).
func (t *lostPacketsTracker) addPacket(seq uint16, timestamp uint32) {
	index := sort.SearchInts(t.packets, int(seq))
	t.packets = append(t.packets, 0)
	t.timestamps = append(t.timestamps, 0)
	copy(t.packets[index+1:], t.packets[index:])
	copy(t.timestamps[index+1:], t.timestamps[index:])
	t.packets[index] = int(seq)
	t.timestamps[index] = timestamp
}

type trackInfoImpl struct {
	trackRemote                           *webrtc.TrackRemote
	rtpPackets, rtpLostPackets, rtpBuffer *atomic.Uint64
	closed                                *atomic.Bool
	mimeType                              string
	kind                                  webrtc.RTPCodecType
}

func newTrackInfo(track *webrtc.TrackRemote, bufferSize time.Duration) TrackInfo {
	info := &trackInfoImpl{
		mimeType:       track.Codec().MimeType,
		kind:           track.Kind(),
		trackRemote:    track,
		rtpPackets:     &atomic.Uint64{},
		rtpBuffer:      &atomic.Uint64{},
		rtpLostPackets: &atomic.Uint64{},
		closed:         &atomic.Bool{},
	}

	go func() {
		//nolint:gosec // G115 TODO: validate CLI limits
		tracker := &lostPacketsTracker{BufferSize: uint32(bufferSize.Milliseconds())}
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				info.setClosed()

				return
			}

			info.increasePacketCount()
			//nolint:gosec // G115 no overflows possible
			info.increaseBufferCount(uint64(len(packet.Payload)) + uint64(packet.Header.MarshalSize()))

			timestamp := packet.Timestamp / (track.Codec().ClockRate / 1000)
			info.increaseLostPacketCount(uint64(tracker.ReportPacket(packet.SequenceNumber, timestamp)))
		}
	}()

	return info
}

func (t *trackInfoImpl) MimeType() string {
	return t.mimeType
}

func (t *trackInfoImpl) Kind() webrtc.RTPCodecType {
	return t.kind
}

func (t *trackInfoImpl) increasePacketCount() {
	t.rtpPackets.Add(1)
}

func (t *trackInfoImpl) increaseBufferCount(size uint64) {
	t.rtpBuffer.Add(size)
}

func (t *trackInfoImpl) increaseLostPacketCount(lost uint64) {
	if lost == 0 {
		return
	}

	t.rtpLostPackets.Add(lost)
}

func (t *trackInfoImpl) GetPacketsCount() uint64 {
	return t.rtpPackets.Load()
}

func (t *trackInfoImpl) GetLostPacketsCount() uint64 {
	return t.rtpLostPackets.Load()
}

func (t *trackInfoImpl) GetBytesCount() uint64 {
	return t.rtpBuffer.Load()
}

// GetTracks returns the info objects of the tracks in the PeerConnection.
func (p *PeerConnection) GetTracks() []TrackInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	copyTracks := make([]TrackInfo, len(p.tracks))
	copy(copyTracks, p.tracks)

	return copyTracks
}

func (t *trackInfoImpl) setClosed() {
	t.closed.Store(true)
}

// IsClosed returns whether the track is closed.
func (t *trackInfoImpl) IsClosed() bool {
	return t.closed.Load()
}
