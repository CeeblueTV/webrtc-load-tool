package webrtc

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

func connectTestPeerConnection(t *testing.T, api *API, offer *PeerConnection) (
	answer *webrtc.PeerConnection,
	videoTrack *webrtc.TrackLocalStaticRTP,
	audioTrack *webrtc.TrackLocalStaticRTP,
) {
	t.Helper()
	answerPC, err := api.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, answerPC)

	videoMimeType := webrtc.MimeTypeH264
	if api.videoCodec == VideoCodecVP8 {
		videoMimeType = webrtc.MimeTypeVP8
	}

	videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    videoMimeType,
		ClockRate:   90000,
		Channels:    0,
		SDPFmtpLine: "",
	}, "video", "webrtc-load-tool-test")
	assert.NoError(t, err)
	audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeOpus,
		ClockRate:   48000,
		Channels:    2,
		SDPFmtpLine: "",
	}, "audio", "webrtc-load-tool-test")
	assert.NoError(t, err)

	audioRTP, err := answerPC.AddTrack(audioTrack)
	assert.NoError(t, err)

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := audioRTP.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	videoRTP, err := answerPC.AddTrack(videoTrack)
	assert.NoError(t, err)

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := videoRTP.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	assert.NoError(t, answerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.GetOffer(),
	}))

	gather := webrtc.GatheringCompletePromise(answerPC)

	answerDesc, err := answerPC.CreateAnswer(nil)
	assert.NoError(t, err)
	assert.NoError(t, answerPC.SetLocalDescription(answerDesc))

	<-gather

	assert.NoError(t, offer.SetAnswer(answerPC.LocalDescription().SDP))

	return answerPC, videoTrack, audioTrack
}

func TestPeerConnectionStatus_String(t *testing.T) {
	states := []PeerConnectionStatus{
		PeerConnectionStatusNew,
		PeerConnectionStatusConnecting,
		PeerConnectionStatusConnected,
		PeerConnectionStatusDisconnected,
		PeerConnectionStatusClosed,
		224,
	}

	for _, state := range states {
		str := state.String()
		assert.NotEmpty(t, str)
	}
}

func TestPeerConnection_state(t *testing.T) {
	pc := PeerConnection{
		state: new(atomic.Int32),
	}

	assert.Equal(t, PeerConnectionStatusNew, pc.GetStatus())
	pc.setStatus(PeerConnectionStatusConnecting)
	assert.Equal(t, PeerConnectionStatusConnecting, pc.GetStatus())
}

func TestPeerConnection_CreatePeerConnectionInvalidConfig(t *testing.T) {
	api, err := newAPI(Configuration{
		iceServers: []webrtc.ICEServer{
			{
				URLs: []string{"fttp:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)

	_, err = api.NewPeerConnection(func(change ChangeCallBackType) {
		if change == ChangeCallBackTypeStateChagne {
			assert.Equal(t, PeerConnectionStatusClosed, change)
		}
	})

	assert.NotNil(t, err)
}

func TestPeerConnection_BuildMediaEngine(t *testing.T) {
	vp8 := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP8,
			ClockRate:    90000,
			Channels:     0,
			SDPFmtpLine:  "",
			RTCPFeedback: nil,
		},
		PayloadType: 102,
	}
	h264 := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeH264,
			ClockRate:    90000,
			Channels:     0,
			SDPFmtpLine:  "",
			RTCPFeedback: nil,
		},
		PayloadType: 96,
	}
	g722 := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeG722,
			ClockRate:    48000,
			Channels:     2,
			SDPFmtpLine:  "",
			RTCPFeedback: nil,
		},
		PayloadType: 111,
	}

	mediaEngine, err := buildMediaEngine(VideoCodecAuto)
	assert.NoError(t, err)
	assert.NotNil(t, mediaEngine)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(vp8, webrtc.RTPCodecTypeVideo), webrtc.ErrCodecAlreadyRegistered)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(h264, webrtc.RTPCodecTypeVideo), webrtc.ErrCodecAlreadyRegistered)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(g722, webrtc.RTPCodecTypeAudio), webrtc.ErrCodecAlreadyRegistered)

	mediaEngine, err = buildMediaEngine(VideoCodecVP8)
	assert.NoError(t, err)
	assert.NotNil(t, mediaEngine)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(h264, webrtc.RTPCodecTypeVideo), webrtc.ErrCodecAlreadyRegistered)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(g722, webrtc.RTPCodecTypeAudio), webrtc.ErrCodecAlreadyRegistered)

	mediaEngine, err = buildMediaEngine(VideoCodecH264)
	assert.NoError(t, err)
	assert.NotNil(t, mediaEngine)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(vp8, webrtc.RTPCodecTypeVideo), webrtc.ErrCodecAlreadyRegistered)
	assert.ErrorIs(t, mediaEngine.RegisterCodec(g722, webrtc.RTPCodecTypeAudio), webrtc.ErrCodecAlreadyRegistered)
}

func TestPeerConnection_Connect(t *testing.T) {
	api, err := newAPI(Configuration{
		iceServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)

	ch := make(chan bool)
	connected := false
	var pc *PeerConnection
	pc, err = api.NewPeerConnection(func(change ChangeCallBackType) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()

			if status == PeerConnectionStatusConnected {
				connected = true
				ch <- true
			}

			if status == PeerConnectionStatusClosed {
				assert.True(t, connected, "PeerConnection should be connected before closed")
			}
		}
	})
	assert.NoError(t, err)
	assert.NotNil(t, pc)
	assert.NotNil(t, pc.pc)

	answer, _, _ := connectTestPeerConnection(t, api, pc)
	assert.NotNil(t, answer)

	<-ch

	assert.NoError(t, pc.pc.Close())
	assert.NoError(t, answer.Close())
}

func TestPeerConnection_Tracks(t *testing.T) {
	api, err := newAPI(Configuration{
		iceServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)

	var pc *PeerConnection
	tracksCount := &atomic.Uint32{}
	ch := make(chan bool)
	pc, err = api.NewPeerConnection(func(change ChangeCallBackType) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()

			if status == PeerConnectionStatusConnected {
				ch <- true
			}
		}

		if change == ChangeCallBackTypeNewTrack {
			if tracksCount.Add(1) >= 2 {
				ch <- true
			}
		}
	})
	assert.NoError(t, err)
	assert.NotNil(t, pc)
	assert.NotNil(t, pc.pc)

	answer, videoTrack, audioTrack := connectTestPeerConnection(t, api, pc)
	assert.NotNil(t, answer)
	assert.NotNil(t, videoTrack)
	assert.NotNil(t, audioTrack)

	<-ch
	time.Sleep(10 * time.Millisecond)
	assert.NoError(t, videoTrack.WriteRTP(&rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         false,
			SequenceNumber: 65535,
			Timestamp:      123456,
		},
		Payload: []byte{0x00, 0x01, 0x02, 0x03},
	}))
	assert.NoError(t, audioTrack.WriteRTP(&rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         true,
			SequenceNumber: 65535,
			Timestamp:      123456,
		},
		Payload: []byte{0x00, 0x01, 0x02, 0x03},
	}))
	time.Sleep(10 * time.Millisecond)
	<-ch

	tracks := pc.GetTracks()

	assert.Len(t, tracks, 2)
	var audio, video *TrackInfo

	for _, track := range tracks {
		switch track.Kind {
		case webrtc.RTPCodecTypeAudio:
			assert.Nil(t, audio, "Got multiple audio tracks")
			audio = track
		case webrtc.RTPCodecTypeVideo:
			assert.Nil(t, video, "Got multiple video tracks")
			video = track
		default:
			assert.Fail(t, "Got unknown track kind", track.Kind)
		}
	}

	assert.Equal(t, uint64(1), video.GetPacketsCount())
	assert.Equal(t, uint64(1), audio.GetPacketsCount())
	assert.NoError(t, pc.pc.Close())
	assert.NoError(t, answer.Close())

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, PeerConnectionStatusClosed, pc.GetStatus())
	assert.True(t, video.IsClosed())
	assert.True(t, audio.IsClosed())
}
