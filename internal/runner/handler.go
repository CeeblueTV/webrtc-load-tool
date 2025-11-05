package runner

import (
	"context"
	"sync"

	"github.com/CeeblueTV/webrtc-load-tool/internal/webrtcpeer"
)

type handler interface {
	// Add a new connection to the runner.
	AddConnection(ctx context.Context) error
}

type handlerCallbacks interface {
	// OnNewConnection is called when a new connection is added.
	OnNewConnection(peer *webrtcpeer.PeerConnection)
	// OnConnecting is called when a connection is connecting.
	OnConnecting(peer *webrtcpeer.PeerConnection)
	// OnConnected is called when a connection is connected.
	OnConnected(peer *webrtcpeer.PeerConnection)
	// OnDisconnected is called when a connection is disconnected.
	OnDisconnected(peer *webrtcpeer.PeerConnection)
	// OnClosed is called when a connection failed (final state).
	OnClosed(peer *webrtcpeer.PeerConnection)
	// OnTrack is called when a track is received.
	OnTrack(track webrtcpeer.TrackInfo)
}

type webrtcHandler struct {
	callbacks    handlerCallbacks
	client       *webrtcpeer.Client
	httpEndpoint string
	peers        []*webrtcpeer.PeerConnection
	mu           sync.RWMutex
}

func newWebRTCHandler(
	config webrtcpeer.Configuration,
	httpEndpoint string,
	callbacks handlerCallbacks,
) (handler, error) {
	handler := &webrtcHandler{
		callbacks:    callbacks,
		peers:        make([]*webrtcpeer.PeerConnection, 0),
		mu:           sync.RWMutex{},
		httpEndpoint: httpEndpoint,
	}
	client, err := webrtcpeer.NewClient(config, handler.changeCallback)
	if err != nil {
		return nil, err
	}

	handler.client = client

	return handler, nil
}

func (h *webrtcHandler) changeCallback(
	peer *webrtcpeer.PeerConnection,
	change webrtcpeer.ChangeCallBackType,
	data any,
) {
	switch change {
	case webrtcpeer.ChangeCallBackTypeStateChagne:
		state, ok := data.(webrtcpeer.PeerConnectionStatus)
		if ok {
			switch state {
			case webrtcpeer.PeerConnectionStatusNew:
				h.callbacks.OnNewConnection(peer)
			case webrtcpeer.PeerConnectionStatusConnecting:
				h.callbacks.OnConnecting(peer)
			case webrtcpeer.PeerConnectionStatusConnected:
				h.callbacks.OnConnected(peer)
			case webrtcpeer.PeerConnectionStatusDisconnected:
				h.callbacks.OnDisconnected(peer)
			case webrtcpeer.PeerConnectionStatusClosed:
				h.callbacks.OnClosed(peer)
			}
		}
	case webrtcpeer.ChangeCallBackTypeNewTrack:
		track, ok := data.(webrtcpeer.TrackInfo)
		if ok {
			h.callbacks.OnTrack(track)
		}
	}
}

func (h *webrtcHandler) AddConnection(ctx context.Context) error {
	peer, err := h.client.NewPeerConnection(ctx, h.httpEndpoint)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = peer.Close()
	}()

	return nil
}
