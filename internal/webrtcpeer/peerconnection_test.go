package webrtcpeer

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

func createTestPair(t *testing.T, client *Client) (
	answer *webrtc.PeerConnection,
	videoTrack *webrtc.TrackLocalStaticRTP,
	audioTrack *webrtc.TrackLocalStaticRTP,
) {
	t.Helper()
	answerPC, err := client.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, answerPC)

	videoMimeType := webrtc.MimeTypeH264
	if client.videoCodec == VideoCodecVP8 {
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

	return answerPC, videoTrack, audioTrack
}

func connectTestPeerConnection(t *testing.T, client *Client, offer *PeerConnection) (
	answer *webrtc.PeerConnection,
	videoTrack *webrtc.TrackLocalStaticRTP,
	audioTrack *webrtc.TrackLocalStaticRTP,
) {
	t.Helper()
	answerPC, videoTrack, audioTrack := createTestPair(t, client)

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
	callback := func(p *PeerConnection, change ChangeCallBackType, data any) {
		if change == ChangeCallBackTypeStateChagne {
			state, ok := data.(PeerConnectionStatus)
			assert.True(t, ok)

			assert.Equal(t, PeerConnectionStatusClosed, state)
			assert.Equal(t, PeerConnectionStatusClosed, p.GetStatus())
		}
	}
	client, err := NewClient(Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"fttp:stun.l.google.com:19302"},
			},
		},
	}, callback)
	assert.NoError(t, err)

	_, err = client.initPeerConnection()

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
	ch := make(chan bool)
	connected := false
	callback := func(pc *PeerConnection, change ChangeCallBackType, data any) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()
			state, ok := data.(PeerConnectionStatus)
			assert.True(t, ok)
			assert.Equal(t, status, state)

			if status == PeerConnectionStatusConnected {
				connected = true
				ch <- true
			}

			if status == PeerConnectionStatusClosed {
				assert.True(t, connected, "PeerConnection should be connected before closed")
			}
		}
	}
	client, err := NewClient(Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}, callback)
	assert.NoError(t, err)
	pc, err := client.initPeerConnection()
	assert.NoError(t, err)
	assert.NotNil(t, pc)
	assert.NotNil(t, pc.pc)

	answer, _, _ := connectTestPeerConnection(t, client, pc)
	assert.NotNil(t, answer)

	<-ch

	assert.NoError(t, pc.pc.Close())
	assert.NoError(t, answer.Close())
}

func TestPeerConnection_Tracks(t *testing.T) {
	tracksCount := &atomic.Uint32{}
	ch := make(chan bool)
	callback := func(pc *PeerConnection, change ChangeCallBackType, data any) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()
			state, ok := data.(PeerConnectionStatus)
			assert.True(t, ok)
			assert.Equal(t, status, state)

			if status == PeerConnectionStatusConnected {
				ch <- true
			}
		}

		if change == ChangeCallBackTypeNewTrack {
			trackInfo, ok := data.(TrackInfo)
			assert.True(t, ok)
			assert.NotNil(t, trackInfo)

			if tracksCount.Add(1) >= 2 {
				ch <- true
			}
		}
	}
	client, err := NewClient(Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}, callback)
	assert.NoError(t, err)
	pc, err := client.initPeerConnection()
	assert.NoError(t, err)
	assert.NotNil(t, pc)
	assert.NotNil(t, pc.pc)

	answer, videoTrack, audioTrack := connectTestPeerConnection(t, client, pc)
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
	var audio, video TrackInfo

	for _, track := range tracks {
		switch track.Kind() {
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

func TestPeerConnection_WHIP(t *testing.T) {
	ch := make(chan bool)
	callback := func(pc *PeerConnection, change ChangeCallBackType, data any) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()
			state, ok := data.(PeerConnectionStatus)
			assert.True(t, ok)
			assert.Equal(t, status, state)

			if status == PeerConnectionStatusConnected {
				ch <- true
			}
		}
	}
	client, err := NewClient(Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}, callback)
	assert.NoError(t, err)
	testserver := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		answer, _, _ := createTestPair(t, client)
		assert.Equal(t, "POST", req.Method)
		mimeType, _, _ := mime.ParseMediaType(req.Header.Get("Accept"))
		assert.Equal(t, "application/sdp", mimeType)
		res.Header().Set("Content-Type", "application/sdp")

		res.WriteHeader(http.StatusCreated)

		body, readErr := io.ReadAll(req.Body)
		assert.NoError(t, readErr)
		assert.NotEmpty(t, body)

		gather := webrtc.GatheringCompletePromise(answer)
		assert.NoError(t, answer.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(body),
		}))
		answerDesc, answerErr := answer.CreateAnswer(nil)
		assert.NoError(t, answerErr)
		assert.NoError(t, answer.SetLocalDescription(answerDesc))
		<-gather

		_, readErr = res.Write([]byte(answer.LocalDescription().SDP))
		assert.NoError(t, readErr)
	}))
	assert.NoError(t, err)
	client.httpClient = testserver.Client()
	pc, err := client.NewPeerConnection(context.Background(), testserver.URL)
	assert.NoError(t, err)
	assert.NotNil(t, pc)

	<-ch
	assert.NoError(t, pc.Close())
}

func TestPeerConnection_WHIPError(t *testing.T) {
	ch := make(chan bool)
	callback := func(pc *PeerConnection, change ChangeCallBackType, data any) {
		if change == ChangeCallBackTypeStateChagne {
			status := pc.GetStatus()
			state, ok := data.(PeerConnectionStatus)
			assert.True(t, ok)
			assert.Equal(t, status, state)

			assert.NotEqual(t, PeerConnectionStatusConnected, status)
			if status == PeerConnectionStatusClosed {
				ch <- true
			}
		}
	}
	client, err := NewClient(Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}, callback)
	assert.NoError(t, err)
	testserver := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "POST", req.Method)
		mimeType, _, _ := mime.ParseMediaType(req.Header.Get("Accept"))
		assert.Equal(t, "application/sdp", mimeType)
		res.Header().Set("Content-Type", "application/sdp")

		res.WriteHeader(http.StatusBadRequest)
	}))
	assert.NoError(t, err)
	client.httpClient = testserver.Client()
	pc, err := client.NewPeerConnection(context.Background(), testserver.URL)
	assert.Error(t, err)
	assert.Nil(t, pc)

	<-ch
}

func TestLostPacketsTracker(t *testing.T) {
	tracker := &lostPacketsTracker{
		BufferDuration: 1000,
	}

	assert.Equal(t, uint(0), tracker.ReportPacket(56, 1000))
	assert.Equal(t, uint(0), tracker.ReportPacket(57, 1000))
	assert.Equal(t, uint(0), tracker.ReportPacket(58, 1000))
	assert.Equal(t, uint(0), tracker.ReportPacket(59, 1000))
	assert.Equal(t, uint(0), tracker.ReportPacket(61, 1050))
	assert.Equal(t, uint(0), tracker.ReportPacket(60, 1040))
	assert.Equal(t, uint(0), tracker.ReportPacket(62, 1600))
	assert.Equal(t, uint(0), tracker.ReportPacket(64, 1600))
	assert.Equal(t, uint(0), tracker.ReportPacket(65, 1800))
	assert.Equal(t, uint(0), tracker.ReportPacket(66, 2000))
	assert.Equal(t, uint(0), tracker.ReportPacket(67, 2500))
	assert.Equal(t, uint(1), tracker.ReportPacket(69, 3000))
	assert.Equal(t, uint(0), tracker.ReportPacket(70, 4000))
	assert.Equal(t, uint(1), tracker.ReportPacket(71, 5000))
	assert.Equal(t, uint(0), tracker.ReportPacket(72, 6000))
	assert.Equal(t, uint(0), tracker.ReportPacket(73, 7000))
	assert.Equal(t, uint(0), tracker.ReportPacket(76, 7500))
	assert.Equal(t, uint(0), tracker.ReportPacket(80, 7500))
	assert.Equal(t, uint(5), tracker.ReportPacket(83, 9000))
	assert.Equal(t, uint(0), tracker.ReportPacket(84, 9000))
	assert.Equal(t, uint(0), tracker.ReportPacket(85, 9500))
	assert.Equal(t, uint(0), tracker.ReportPacket(86, 10000))
	assert.Equal(t, uint(2), tracker.ReportPacket(87, 10020))
}
