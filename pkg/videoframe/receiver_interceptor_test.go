// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceiverInterceptor_Factory(t *testing.T) {
	// Factory should create a new ReceiverInterceptor

	factory, err := NewReceiverInterceptor()
	require.NoError(t, err)
	require.NotNil(t, factory)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)
	require.NotNil(t, i)

	err = i.Close()
	assert.NoError(t, err)
}

func TestReceiverInterceptor_VP8SingleFrame(t *testing.T) {
	// Single packet VP8 frame should be assembled and available via Attributes

	factory, err := NewReceiverInterceptor()
	require.NoError(t, err)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)
	defer func() { _ = i.Close() }()

	info := &interceptor.StreamInfo{
		SSRC:         123456,
		ClockRate:    90000,
		MimeType:     "video/VP8",
		PayloadType:  96,
		RTPHeaderExtensions: nil,
	}

	// Create VP8 payload (S=1, PID=0 for first packet)
	vp8Payload := createVP8Payload(true, 42)

	// Track captured frames
	var capturedFrame *EncodedFrame
	frameCaptureChan := make(chan *EncodedFrame, 1)

	reader := i.BindRemoteStream(info, interceptor.RTPReaderFunc(
		func(b []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
			pkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    96,
					SequenceNumber: 1000,
					Timestamp:      90000,
					SSRC:           123456,
					Marker:         true, // Single packet = last packet
				},
				Payload: vp8Payload,
			}
			data, _ := pkt.Marshal()
			copy(b, data)
			return len(data), attrs, nil
		},
	))

	// Read packet and check for frame in attributes
	buf := make([]byte, 1500)
	_, attrs, err := reader.Read(buf, interceptor.Attributes{})
	require.NoError(t, err)
	require.NotNil(t, attrs)

	// Check EncodedFramesKey (primary)
	frames, ok := attrs.Get(EncodedFramesKey).([]*EncodedFrame)
	require.True(t, ok, "EncodedFramesKey should be present")
	require.Len(t, frames, 1, "Should have 1 frame")
	assert.Equal(t, uint32(90000), frames[0].Timestamp)

	// Check EncodedFrameKey (backward compatibility)
	if frame, ok := attrs.Get(EncodedFrameKey).(*EncodedFrame); ok {
		capturedFrame = frame
		select {
		case frameCaptureChan <- frame:
		default:
		}
	}
	require.NotNil(t, capturedFrame, "EncodedFrameKey should be present for backward compatibility")
	assert.Equal(t, uint32(90000), capturedFrame.Timestamp)
}

func TestReceiverInterceptor_VP8MultiPacketFrame(t *testing.T) {
	// Multi-packet VP8 frame should be assembled when last packet arrives

	factory, err := NewReceiverInterceptor()
	require.NoError(t, err)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)
	defer func() { _ = i.Close() }()

	info := &interceptor.StreamInfo{
		SSRC:        123456,
		ClockRate:   90000,
		MimeType:    "video/VP8",
		PayloadType: 96,
	}

	// Create sequence of VP8 packets
	packets := []*rtp.Packet{
		{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: 1000,
				Timestamp:      90000,
				SSRC:           123456,
				Marker:         false, // Not last
			},
			Payload: createVP8Payload(true, 42), // First packet
		},
		{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: 1001,
				Timestamp:      90000,
				SSRC:           123456,
				Marker:         false, // Not last
			},
			Payload: createVP8PayloadMiddle(),
		},
		{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: 1002,
				Timestamp:      90000,
				SSRC:           123456,
				Marker:         true, // Last packet
			},
			Payload: createVP8PayloadMiddle(),
		},
	}

	packetIdx := 0
	reader := i.BindRemoteStream(info, interceptor.RTPReaderFunc(
		func(b []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
			if packetIdx >= len(packets) {
				return 0, attrs, nil
			}
			pkt := packets[packetIdx]
			packetIdx++
			data, _ := pkt.Marshal()
			copy(b, data)
			return len(data), attrs, nil
		},
	))

	var capturedFrames []*EncodedFrame
	buf := make([]byte, 1500)

	// Read first two packets - should not complete frame
	for j := 0; j < 2; j++ {
		_, attrs, err := reader.Read(buf, interceptor.Attributes{})
		require.NoError(t, err)
		if attrs != nil {
			if frames, ok := attrs.Get(EncodedFramesKey).([]*EncodedFrame); ok {
				capturedFrames = frames
			}
		}
	}
	assert.Nil(t, capturedFrames, "Frame should not be complete before last packet")

	// Read last packet - should complete frame
	_, attrs, err := reader.Read(buf, interceptor.Attributes{})
	require.NoError(t, err)
	require.NotNil(t, attrs)

	// Check EncodedFramesKey (primary)
	frames, ok := attrs.Get(EncodedFramesKey).([]*EncodedFrame)
	require.True(t, ok, "EncodedFramesKey should be present")
	require.Len(t, frames, 1, "Should have 1 frame")
	assert.Equal(t, uint32(90000), frames[0].Timestamp)

	// Check EncodedFrameKey (backward compatibility)
	frame, ok := attrs.Get(EncodedFrameKey).(*EncodedFrame)
	require.True(t, ok, "EncodedFrameKey should be present")
	require.NotNil(t, frame, "Frame should be complete after last packet")
	assert.Equal(t, uint32(90000), frame.Timestamp)
}

func TestReceiverInterceptor_NonVP8Passthrough(t *testing.T) {
	// Non-VP8 streams should pass through without modification

	factory, err := NewReceiverInterceptor()
	require.NoError(t, err)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)
	defer func() { _ = i.Close() }()

	info := &interceptor.StreamInfo{
		SSRC:        123456,
		ClockRate:   48000,
		MimeType:    "audio/opus",
		PayloadType: 111,
	}

	reader := i.BindRemoteStream(info, interceptor.RTPReaderFunc(
		func(b []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
			pkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    111,
					SequenceNumber: 1000,
					Timestamp:      48000,
					SSRC:           123456,
				},
				Payload: []byte{0x01, 0x02, 0x03},
			}
			data, _ := pkt.Marshal()
			copy(b, data)
			return len(data), attrs, nil
		},
	))

	buf := make([]byte, 1500)
	n, attrs, err := reader.Read(buf, interceptor.Attributes{})
	require.NoError(t, err)
	assert.Greater(t, n, 0)

	// Non-VP8 should not have EncodedFrame in attributes
	if attrs != nil {
		frames := attrs.Get(EncodedFramesKey)
		assert.Nil(t, frames, "EncodedFramesKey should not be present for non-VP8")
		frame := attrs.Get(EncodedFrameKey)
		assert.Nil(t, frame, "EncodedFrameKey should not be present for non-VP8")
	}
}

func TestReceiverInterceptor_WithOptions(t *testing.T) {
	// Factory should accept options

	factory, err := NewReceiverInterceptor(
		WithPacketBufferSize(1024),
	)
	require.NoError(t, err)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)
	require.NotNil(t, i)

	err = i.Close()
	assert.NoError(t, err)
}

func TestReceiverInterceptor_Close(t *testing.T) {
	// Close should clean up resources

	factory, err := NewReceiverInterceptor()
	require.NoError(t, err)

	i, err := factory.NewInterceptor("")
	require.NoError(t, err)

	info := &interceptor.StreamInfo{
		SSRC:        123456,
		ClockRate:   90000,
		MimeType:    "video/VP8",
		PayloadType: 96,
	}

	_ = i.BindRemoteStream(info, interceptor.RTPReaderFunc(
		func(b []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
			return 0, attrs, nil
		},
	))

	err = i.Close()
	assert.NoError(t, err)
}

// Helper functions for creating VP8 test payloads

func createVP8Payload(isFirst bool, pictureID int) []byte {
	// Create a VP8 payload that pion/rtp/codecs can unmarshal
	// VP8 payload descriptor:
	// X R N S R PID  (1 byte)
	// I L T K RSV   (extension byte, if X=1)
	// PictureID     (if I=1)

	var payload []byte

	if isFirst {
		// S=1 (start of partition), PID=0
		if pictureID >= 0 {
			// X=1, S=1, PID=0
			payload = []byte{0x90} // X=1, S=1
			// Extension: I=1
			payload = append(payload, 0x80) // I=1
			if pictureID > 127 {
				// 15-bit picture ID
				payload = append(payload, byte(0x80|((pictureID>>8)&0x7F)))
				payload = append(payload, byte(pictureID&0xFF))
			} else {
				// 7-bit picture ID
				payload = append(payload, byte(pictureID&0x7F))
			}
		} else {
			// S=1, PID=0, no extension
			payload = []byte{0x10} // S=1
		}
	} else {
		// S=0 for continuation
		payload = []byte{0x00}
	}

	// Add some dummy payload data
	payload = append(payload, []byte{0xAA, 0xBB, 0xCC}...)

	return payload
}

func createVP8PayloadMiddle() []byte {
	// Middle packet: S=0, PID=0
	return []byte{0x00, 0xDD, 0xEE, 0xFF}
}

// Test that VP8 packet can be unmarshaled
func TestVP8PacketUnmarshal(t *testing.T) {
	payload := createVP8Payload(true, 42)
	vp8 := &codecs.VP8Packet{}
	_, err := vp8.Unmarshal(payload)
	require.NoError(t, err)
	assert.Equal(t, uint8(1), vp8.S, "S bit should be 1 for first packet")
	assert.Equal(t, uint8(0), vp8.PID, "PID should be 0 for first packet")
	assert.Equal(t, uint16(42), vp8.PictureID)
}
