package runner

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CeeblueTV/webrtc-load-tool/internal/webrtcpeer"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

type testCallbackType int8

const (
	testCallbackTypeOnNewConnection testCallbackType = iota
	testCallbackTypeOnConnecting
	testCallbackTypeOnConnected
	testCallbackTypeOnDisconnected
	testCallbackTypeOnClosed
	testCallbackTypeOnTrack
)

type testCallbacks struct {
	ch     chan<- testCallbackType
	tracks chan<- webrtcpeer.TrackInfo
}

func (t testCallbacks) OnNewConnection(_ *webrtcpeer.PeerConnection) {
	t.ch <- testCallbackTypeOnNewConnection
}

func (t testCallbacks) OnConnecting(_ *webrtcpeer.PeerConnection) {
	t.ch <- testCallbackTypeOnConnecting
}

func (t testCallbacks) OnConnected(_ *webrtcpeer.PeerConnection) {
	t.ch <- testCallbackTypeOnConnected
}

func (t testCallbacks) OnDisconnected(_ *webrtcpeer.PeerConnection) {
	t.ch <- testCallbackTypeOnDisconnected
}

func (t testCallbacks) OnClosed(_ *webrtcpeer.PeerConnection) {
	t.ch <- testCallbackTypeOnClosed
}

func (t testCallbacks) OnTrack(track webrtcpeer.TrackInfo) {
	t.tracks <- track
}

func createTestPair(t *testing.T) (
	answer *webrtc.PeerConnection,
	videoTrack *webrtc.TrackLocalStaticRTP,
	audioTrack *webrtc.TrackLocalStaticRTP,
) {
	t.Helper()
	answerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, answerPC)

	videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
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

func TestWebRTCHandler(t *testing.T) {
	closeCh := make(chan bool)
	whip := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		answer, videoTrack, audioTrack := createTestPair(t)
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
		go func() {
			<-closeCh
			assert.NoError(t, answer.Close())
		}()

		go func() {
			for answer.ConnectionState() != webrtc.PeerConnectionStateDisconnected {
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
			}
		}()
	})
	server := httptest.NewServer(whip)
	defer server.Close()

	ch := make(chan testCallbackType)
	trackCh := make(chan webrtcpeer.TrackInfo)
	callbacks := testCallbacks{
		ch:     ch,
		tracks: trackCh,
	}
	handler, err := newWebRTCHandler(webrtcpeer.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}, server.URL, callbacks)
	assert.NoError(t, err)

	assert.NoError(t, handler.AddConnection(context.Background()))
	assert.Equal(t, testCallbackTypeOnNewConnection, <-ch)
	assert.Equal(t, testCallbackTypeOnConnecting, <-ch)
	assert.Equal(t, testCallbackTypeOnConnected, <-ch)
	firstTrack := <-trackCh
	secondTrack := <-trackCh
	assert.NotEqual(t, firstTrack.Kind(), secondTrack.Kind(), "Expected a video and an audio track")
	closeCh <- true
	assert.Equal(t, testCallbackTypeOnClosed, <-ch)

	ctx, cancel := context.WithCancel(context.Background())
	assert.NoError(t, handler.AddConnection(ctx))
	assert.Equal(t, testCallbackTypeOnNewConnection, <-ch)
	assert.Equal(t, testCallbackTypeOnConnecting, <-ch)
	assert.Equal(t, testCallbackTypeOnConnected, <-ch)
	firstTrack = <-trackCh
	secondTrack = <-trackCh
	assert.NotEqual(t, firstTrack.Kind(), secondTrack.Kind(), "Expected a video and an audio track")
	cancel()
	assert.Equal(t, testCallbackTypeOnClosed, <-ch)
}
