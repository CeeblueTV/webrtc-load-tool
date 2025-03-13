package webrtc

import (
	"context"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildWhipRequest_InvalidParameters(t *testing.T) {
	req, err := buildWhipRequest(context.TODO(), "https://ceeblue.net/", "")
	assert.ErrorIs(t, err, errEmptySDP)
	assert.Nil(t, req)

	//nolint:staticcheck // We are testing if it returns `NewRequestWithContext` errors.
	req, err = buildWhipRequest(nil, "https://ceeblue.net/", "offer")
	assert.Error(t, err)
	assert.Nil(t, req)
}

func TestBuildWhipRequest(t *testing.T) {
	url := "https://ceeblue.net/"
	req, err := buildWhipRequest(context.TODO(), url, "offer")
	assert.NoError(t, err)
	assert.NotNil(t, req)

	assert.Equal(t, url, req.URL.String())
	assert.Equal(t, "POST", req.Method)

	mimeType, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type"))
	assert.Equal(t, "application/sdp", mimeType)
	mimeType, _, _ = mime.ParseMediaType(req.Header.Get("Accept"))
	assert.Equal(t, "application/sdp", mimeType)
}

func TestWhip(t *testing.T) {
	offer := "v=0\no=- 0 0 IN IP4"
	answer := "v=0\no=- 1 1 IN IP4"
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		mimeType, _, _ := mime.ParseMediaType(r.Header.Get("Accept"))
		assert.Equal(t, "application/sdp", mimeType)
		w.Header().Set("Content-Type", "application/sdp")

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(answer))
	}))
	res, err := whip(context.TODO(), testServer.Client(), testServer.URL, offer)
	assert.NoError(t, err)
	assert.Equal(t, answer, res)
}

func TestWhip_BadRequests(t *testing.T) {
	badCases := []struct {
		ExpectedError   error
		ResponseHeaders map[string]string
		Name            string
		Offer           string
		Answer          string
		ResponseCode    int
		Hijack          bool
		Close           bool
	}{
		{
			Name:   "Empty offer",
			Answer: "v=0\no=- 0 0 IN IP4",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/sdp",
			},
			ResponseCode:  http.StatusCreated,
			ExpectedError: errEmptySDP,
		},
		{
			Name:   "Server closed",
			Offer:  "v=0\no=- 0 0 IN IP4",
			Answer: "v=0\no=- 0 0 IN IP4",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/sdp",
			},
			ResponseCode: http.StatusCreated,
			Close:        true,
		},
		{
			Name:   "Invalid response code",
			Offer:  "v=0\no=- 0 0 IN IP4",
			Answer: "v=0\no=- 0 0 IN IP4",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/sdp",
			},
			ResponseCode:  http.StatusOK,
			ExpectedError: errInvalidWHIPResponseCode,
		},
		{
			Name:   "Invalid response content-type",
			Offer:  "v=0\no=- 0 0 IN IP4",
			Answer: "v=0\no=- 0 0 IN IP4",
			ResponseHeaders: map[string]string{
				"Content-Type": "plain/text",
			},
			ResponseCode:  http.StatusCreated,
			ExpectedError: errInvalidWhipResponse,
		},
		{
			Name:   "Fail to read response",
			Offer:  "v=0\no=- 0 0 IN IP4",
			Answer: "v=0\no=- 0 0 IN IP4",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/sdp",
			},
			ResponseCode: http.StatusCreated,
			Hijack:       true,
		},
		{
			Name:   "Empty response",
			Offer:  "v=0\no=- 0 0 IN IP4",
			Answer: "",
			ResponseHeaders: map[string]string{
				"Content-Type": "application/sdp",
			},
			ResponseCode:  http.StatusCreated,
			ExpectedError: errInvalidWhipResponse,
		},
	}

	for _, testCase := range badCases {
		t.Run(testCase.Name, func(t *testing.T) {
			testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				assert.Equal(t, "POST", req.Method)
				mimeType, _, _ := mime.ParseMediaType(req.Header.Get("Accept"))
				assert.Equal(t, "application/sdp", mimeType)
				for k, v := range testCase.ResponseHeaders {
					res.Header().Set(k, v)
				}

				res.WriteHeader(testCase.ResponseCode)

				if testCase.Hijack {
					hijack, ok := res.(http.Hijacker)
					assert.True(t, ok)
					conn, _, err := hijack.Hijack()
					assert.NoError(t, err)
					_ = conn.Close()

					return
				}

				i, err := res.Write([]byte(testCase.Answer))
				assert.NoError(t, err)
				assert.Equal(t, len(testCase.Answer), i)
			}))

			if testCase.Close {
				testServer.Close()
			}

			res, err := whip(context.TODO(), testServer.Client(), testServer.URL, testCase.Offer)
			if testCase.ExpectedError == nil {
				assert.Error(t, err)
			} else {
				assert.ErrorIs(t, err, testCase.ExpectedError)
			}

			assert.Equal(t, "", res)
		})
	}
}
