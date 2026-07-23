package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateVideoProxyPayload(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		prefix      []byte
		wantError   bool
	}{
		{
			name:        "mp4",
			contentType: "video/mp4",
			prefix:      []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
		},
		{
			name:        "hls playlist",
			contentType: "application/vnd.apple.mpegurl",
			prefix:      []byte("#EXTM3U\n#EXT-X-VERSION:3\n"),
		},
		{
			name:        "json content type",
			contentType: "application/json",
			prefix:      []byte(`{"error":{"message":"provider secret"}}`),
			wantError:   true,
		},
		{
			name:        "json disguised as video",
			contentType: "video/mp4",
			prefix:      []byte(`{"error":{"message":"provider secret"}}`),
			wantError:   true,
		},
		{
			name:        "plain text disguised as binary",
			contentType: "application/octet-stream",
			prefix:      []byte("all nodes failed to stream provider secret"),
			wantError:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateVideoProxyPayload(test.contentType, test.prefix)
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
