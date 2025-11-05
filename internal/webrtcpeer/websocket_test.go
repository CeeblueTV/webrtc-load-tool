package webrtcpeer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindBestTrackIndex(t *testing.T) {
	tests := []struct {
		name         string
		tracks       []resolutionTrack
		targetHeight int
		targetCodec  string
		wantIdx      int
		wantFound    bool
	}{
		{
			name:         "empty tracks",
			tracks:       []resolutionTrack{},
			targetHeight: 720,
			wantFound:    false,
		},
		{
			name: "exact match",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 480, Codec: "H264"},
				{Idx: 1, Height: 720, Codec: "H264"},
				{Idx: 2, Height: 1080, Codec: "H264"},
			},
			targetHeight: 720,
			wantIdx:      1,
			wantFound:    true,
		},
		{
			name: "closest below target",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 480, Codec: "H264"},
				{Idx: 1, Height: 720, Codec: "H264"},
				{Idx: 2, Height: 1080, Codec: "H264"},
			},
			targetHeight: 800,
			wantIdx:      1,
			wantFound:    true,
		},
		{
			name: "no track below target, use lowest",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 1080, Codec: "H264"},
				{Idx: 1, Height: 1440, Codec: "H264"},
				{Idx: 2, Height: 2160, Codec: "H264"},
			},
			targetHeight: 720,
			wantIdx:      0,
			wantFound:    true,
		},
		{
			name: "codec filter",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 720, Codec: "VP8"},
				{Idx: 1, Height: 720, Codec: "H264"},
				{Idx: 2, Height: 1080, Codec: "H264"},
			},
			targetHeight: 1080,
			targetCodec:  "H264",
			wantIdx:      2,
			wantFound:    true,
		},
		{
			name: "codec filter case insensitive",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 720, Codec: "vp8"},
				{Idx: 1, Height: 720, Codec: "h264"},
				{Idx: 2, Height: 1080, Codec: "H264"},
			},
			targetHeight: 1080,
			targetCodec:  "H264",
			wantIdx:      2,
			wantFound:    true,
		},
		{
			name: "no matching codec",
			tracks: []resolutionTrack{
				{Idx: 0, Height: 720, Codec: "VP8"},
				{Idx: 1, Height: 1080, Codec: "VP8"},
			},
			targetHeight: 1080,
			targetCodec:  "H264",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, found := findBestTrackIndex(tt.tracks, tt.targetHeight, tt.targetCodec)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantIdx, idx)
			}
		})
	}
}

func TestMatchesCodec(t *testing.T) {
	tests := []struct {
		name        string
		codec       string
		targetCodec string
		want        bool
	}{
		{"empty target matches all", "H264", "", true},
		{"exact match", "H264", "H264", true},
		{"case insensitive", "h264", "H264", true},
		{"case insensitive reverse", "H264", "h264", true},
		{"no match", "VP8", "H264", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesCodec(tt.codec, tt.targetCodec)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractTracksFromMetadata(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantTracks []resolutionTrack
	}{
		{
			name: "valid metadata with video tracks",
			data: `{
				"meta": {
					"tracks": {
						"0": {
							"idx": 0,
							"trackid": 10,
							"width": 640,
							"height": 480,
							"codec": "H264",
							"type": "video"
						},
						"1": {
							"idx": 1,
							"trackid": 11,
							"width": 1280,
							"height": 720,
							"codec": "H264",
							"type": "video"
						},
						"2": {
							"idx": 2,
							"trackid": 12,
							"width": 1920,
							"height": 1080,
							"codec": "VP8",
							"type": "audio"
						}
					}
				}
			}`,
			wantTracks: []resolutionTrack{
				{Idx: 0, Trackid: 10, Width: 640, Height: 480, Codec: "H264", Type: "video"},
				{Idx: 1, Trackid: 11, Width: 1280, Height: 720, Codec: "H264", Type: "video"},
			},
		},
		{
			name:       "invalid JSON",
			data:       `invalid json`,
			wantTracks: nil,
		},
		{
			name: "no video tracks",
			data: `{
				"meta": {
					"tracks": {
						"0": {
							"idx": 0,
							"trackid": 10,
							"type": "audio"
						}
					}
				}
			}`,
			wantTracks: []resolutionTrack{},
		},
		{
			name:       "empty metadata",
			data:       `{}`,
			wantTracks: []resolutionTrack{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracks := extractTracksFromMetadata([]byte(tt.data))
			assert.Equal(t, tt.wantTracks, tracks)
		})
	}
}

func TestDeriveWSURLs(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantSig  string
		wantMeta string
	}{
		{
			name:     "webrtc endpoint",
			endpoint: "ws://example.com/webrtc/out+123",
			wantSig:  "ws://example.com/webrtc/out+123",
			wantMeta: "ws://example.com/json_out+123.js",
		},
		{
			name:     "json_out endpoint",
			endpoint: "ws://example.com/json_out+123.js",
			wantSig:  "ws://example.com/webrtc/out+123",
			wantMeta: "ws://example.com/json_out+123.js",
		},
		{
			name:     "default fallback",
			endpoint: "ws://example.com/some/path",
			wantSig:  "ws://example.com/some/path",
			wantMeta: "ws://example.com/some/path.json",
		},
		{
			name:     "invalid URL",
			endpoint: "not a url",
			wantSig:  "not%20a%20url",
			wantMeta: "not%20a%20url.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, meta := deriveWSURLs(tt.endpoint)
			assert.Equal(t, tt.wantSig, sig)
			assert.Equal(t, tt.wantMeta, meta)
		})
	}
}

func TestResolutionTracker_UpdateTracks(t *testing.T) {
	t.Run("updates available tracks", func(t *testing.T) {
		tracker := newResolutionTracker(720, "H264", nil)

		tracks := []resolutionTrack{
			{Idx: 0, Height: 480, Codec: "H264"},
			{Idx: 1, Height: 720, Codec: "H264"},
			{Idx: 2, Height: 1080, Codec: "H264"},
		}

		tracker.updateTracks(tracks)

		assert.Equal(t, tracks, tracker.availableTracks)
	})

	t.Run("no switch if already on best track", func(t *testing.T) {
		tracker := newResolutionTracker(720, "H264", nil)

		tracks := []resolutionTrack{
			{Idx: 0, Height: 480, Codec: "H264"},
			{Idx: 1, Height: 720, Codec: "H264"},
		}

		tracker.mu.Lock()
		tracker.currentIdx = 1
		tracker.mu.Unlock()

		tracker.updateTracks(tracks)

		assert.Equal(t, 1, tracker.currentIdx)
	})

	t.Run("respects cooldown", func(t *testing.T) {
		tracker := newResolutionTracker(720, "H264", nil)

		tracks := []resolutionTrack{
			{Idx: 0, Height: 480, Codec: "H264"},
			{Idx: 1, Height: 720, Codec: "H264"},
		}

		tracker.updateTracks(tracks)
		firstIdx := tracker.currentIdx

		tracker.updateTracks(tracks)

		assert.Equal(t, firstIdx, tracker.currentIdx)
	})

	t.Run("disabled when targetHeight is 0", func(t *testing.T) {
		tracker := newResolutionTracker(0, "H264", nil)

		tracks := []resolutionTrack{
			{Idx: 0, Height: 480, Codec: "H264"},
			{Idx: 1, Height: 720, Codec: "H264"},
		}

		tracker.updateTracks(tracks)

		assert.Equal(t, 0, tracker.currentIdx)
	})

	t.Run("empty tracks ignored", func(t *testing.T) {
		tracker := newResolutionTracker(720, "H264", nil)

		tracker.updateTracks([]resolutionTrack{})

		assert.Equal(t, 0, tracker.currentIdx)
	})
}

func TestNewResolutionTracker(t *testing.T) {
	t.Run("defaults to H264 when codec not specified", func(t *testing.T) {
		tracker := newResolutionTracker(720, "", nil)
		assert.Equal(t, "H264", tracker.targetCodec)
		assert.Equal(t, 720, tracker.targetHeight)
	})

	t.Run("does not default when resolution is 0", func(t *testing.T) {
		tracker := newResolutionTracker(0, "", nil)
		assert.Equal(t, "", tracker.targetCodec)
	})

	t.Run("uses provided codec", func(t *testing.T) {
		tracker := newResolutionTracker(720, "VP8", nil)
		assert.Equal(t, "VP8", tracker.targetCodec)
	})
}

func TestResolutionTracker_FindBestTrack(t *testing.T) {
	tracker := newResolutionTracker(720, "H264", nil)

	tracks := []resolutionTrack{
		{Idx: 0, Height: 480, Codec: "H264"},
		{Idx: 1, Height: 720, Codec: "H264"},
		{Idx: 2, Height: 1080, Codec: "H264"},
	}

	tracker.mu.Lock()
	tracker.availableTracks = tracks
	tracker.mu.Unlock()

	idx, found := tracker.findBestTrack()
	require.True(t, found)
	assert.Equal(t, 1, idx)
}
