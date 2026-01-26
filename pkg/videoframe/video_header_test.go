// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/pion/rtp/codecs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRTPVideoHeader_VP8_FirstPacket(t *testing.T) {
	// VP8 S=1, PID=0 → IsFirstPacketInFrame = true
	// RFC 7741: S bit indicates start of VP8 partition
	// First packet of frame has S=1 and PID=0

	vp8Pkt := &codecs.VP8Packet{
		S:   1,   // Start of partition
		PID: 0,   // Partition ID = 0
	}

	header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

	require.NotNil(t, header)
	assert.True(t, header.IsFirstPacketInFrame, "S=1, PID=0 should mark first packet in frame")
	assert.False(t, header.IsLastPacketInFrame, "marker=false should not mark last packet")
}

func TestRTPVideoHeader_VP8_NotFirstPacket(t *testing.T) {
	tests := []struct {
		name     string
		s        uint8
		pid      uint8
		expected bool
	}{
		{
			name:     "S=0 is not first packet",
			s:        0,
			pid:      0,
			expected: false,
		},
		{
			name:     "S=1 but PID!=0 is not first packet",
			s:        1,
			pid:      1,
			expected: false,
		},
		{
			name:     "S=0 and PID!=0 is not first packet",
			s:        0,
			pid:      2,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp8Pkt := &codecs.VP8Packet{
				S:   tt.s,
				PID: tt.pid,
			}

			header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

			require.NotNil(t, header)
			assert.Equal(t, tt.expected, header.IsFirstPacketInFrame)
		})
	}
}

func TestRTPVideoHeader_VP8_LastPacket(t *testing.T) {
	tests := []struct {
		name     string
		marker   bool
		expected bool
	}{
		{
			name:     "marker=true is last packet",
			marker:   true,
			expected: true,
		},
		{
			name:     "marker=false is not last packet",
			marker:   false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp8Pkt := &codecs.VP8Packet{
				S:   1,
				PID: 0,
			}

			header := NewRTPVideoHeaderFromVP8(vp8Pkt, tt.marker)

			require.NotNil(t, header)
			assert.Equal(t, tt.expected, header.IsLastPacketInFrame)
		})
	}
}

func TestRTPVideoHeader_VP8_PictureID_7bit(t *testing.T) {
	// VP8 with 7-bit PictureID (M=0)
	vp8Pkt := &codecs.VP8Packet{
		S:         1,
		PID:       0,
		I:         1, // PictureID present
		PictureID: 42, // 7-bit value
	}

	header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

	require.NotNil(t, header)
	assert.Equal(t, int32(42), header.PictureID)
}

func TestRTPVideoHeader_VP8_PictureID_15bit(t *testing.T) {
	// VP8 with 15-bit PictureID (M=1)
	vp8Pkt := &codecs.VP8Packet{
		S:         1,
		PID:       0,
		I:         1,     // PictureID present
		PictureID: 16000, // 15-bit value
	}

	header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

	require.NotNil(t, header)
	assert.Equal(t, int32(16000), header.PictureID)
}

func TestRTPVideoHeader_VP8_NoPictureID(t *testing.T) {
	// VP8 without PictureID
	vp8Pkt := &codecs.VP8Packet{
		S:   1,
		PID: 0,
		I:   0, // PictureID not present
	}

	header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

	require.NotNil(t, header)
	assert.Equal(t, NoPictureID, header.PictureID)
}

func TestRTPVideoHeader_VP8_TemporalIdx(t *testing.T) {
	tests := []struct {
		name        string
		t           uint8
		tid         uint8
		expectedIdx int8
	}{
		{
			name:        "T=1 with TID=0",
			t:           1,
			tid:         0,
			expectedIdx: 0,
		},
		{
			name:        "T=1 with TID=1",
			t:           1,
			tid:         1,
			expectedIdx: 1,
		},
		{
			name:        "T=1 with TID=2",
			t:           1,
			tid:         2,
			expectedIdx: 2,
		},
		{
			name:        "T=0 means no temporal info",
			t:           0,
			tid:         0,
			expectedIdx: NoTemporalIdx,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp8Pkt := &codecs.VP8Packet{
				S:   1,
				PID: 0,
				T:   tt.t,
				TID: tt.tid,
			}

			header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

			require.NotNil(t, header)
			assert.Equal(t, tt.expectedIdx, header.TemporalIdx)
		})
	}
}

func TestRTPVideoHeader_VP8_TL0PicIdx(t *testing.T) {
	tests := []struct {
		name        string
		l           uint8
		tl0PicIdx   uint8
		expectedIdx int16
	}{
		{
			name:        "L=1 with TL0PICIDX=0",
			l:           1,
			tl0PicIdx:   0,
			expectedIdx: 0,
		},
		{
			name:        "L=1 with TL0PICIDX=100",
			l:           1,
			tl0PicIdx:   100,
			expectedIdx: 100,
		},
		{
			name:        "L=0 means no TL0PICIDX",
			l:           0,
			tl0PicIdx:   50,
			expectedIdx: NoTL0PicIdx,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp8Pkt := &codecs.VP8Packet{
				S:         1,
				PID:       0,
				L:         tt.l,
				TL0PICIDX: tt.tl0PicIdx,
			}

			header := NewRTPVideoHeaderFromVP8(vp8Pkt, false)

			require.NotNil(t, header)
			assert.Equal(t, tt.expectedIdx, header.TL0PicIdx)
		})
	}
}
