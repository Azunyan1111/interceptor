// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoPacketBuffer_InsertSinglePacketFrame(t *testing.T) {
	// Single packet frame: first=true, last=true
	// Should return frame immediately upon insertion

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)
	require.NotNil(t, buffer)

	// Create a single-packet frame (first and last packet)
	pkt := &BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01, 0x02, 0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  true,
		},
	}

	result := buffer.InsertPacket(pkt)

	// Single packet frame should be returned immediately
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 1, "Single packet frame should have 1 packet")
	assert.Equal(t, int64(1000), result.Frames[0][0].SequenceNumber)
}

func TestVideoPacketBuffer_InsertMultiPacketFrame(t *testing.T) {
	// Multi-packet frame: first=true -> middle -> last=true
	// Should return frame when last packet arrives

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	// First packet
	pkt1 := &BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	}

	result := buffer.InsertPacket(pkt1)
	assert.Len(t, result.Frames, 0, "First packet alone should not complete frame")

	// Middle packet
	pkt2 := &BufferedPacket{
		SequenceNumber: 1001,
		Timestamp:      90000,
		Payload:        []byte{0x02},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  false,
		},
	}

	result = buffer.InsertPacket(pkt2)
	assert.Len(t, result.Frames, 0, "Middle packet should not complete frame")

	// Last packet
	pkt3 := &BufferedPacket{
		SequenceNumber: 1002,
		Timestamp:      90000,
		Payload:        []byte{0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	}

	result = buffer.InsertPacket(pkt3)
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 3, "Frame should have 3 packets")

	// Verify packet order
	assert.Equal(t, int64(1000), result.Frames[0][0].SequenceNumber)
	assert.Equal(t, int64(1001), result.Frames[0][1].SequenceNumber)
	assert.Equal(t, int64(1002), result.Frames[0][2].SequenceNumber)
}

func TestVideoPacketBuffer_OutOfOrderPackets(t *testing.T) {
	// Out of order: seq=2 -> seq=1 -> seq=3
	// Frame should complete when continuity is established

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	// Second packet arrives first
	pkt2 := &BufferedPacket{
		SequenceNumber: 1001,
		Timestamp:      90000,
		Payload:        []byte{0x02},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  false,
		},
	}
	result := buffer.InsertPacket(pkt2)
	assert.Len(t, result.Frames, 0)

	// First packet arrives
	pkt1 := &BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	}
	result = buffer.InsertPacket(pkt1)
	assert.Len(t, result.Frames, 0)

	// Last packet arrives - should complete frame
	pkt3 := &BufferedPacket{
		SequenceNumber: 1002,
		Timestamp:      90000,
		Payload:        []byte{0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	}
	result = buffer.InsertPacket(pkt3)
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 3, "Frame should complete despite out of order arrival")

	// Verify packets are in sequence order
	assert.Equal(t, int64(1000), result.Frames[0][0].SequenceNumber)
	assert.Equal(t, int64(1001), result.Frames[0][1].SequenceNumber)
	assert.Equal(t, int64(1002), result.Frames[0][2].SequenceNumber)
}

func TestVideoPacketBuffer_MissingPacket(t *testing.T) {
	// Missing packet: seq=1(first) -> seq=3(last)
	// Frame should NOT complete until seq=2 arrives

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	// First packet
	pkt1 := &BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	}
	result := buffer.InsertPacket(pkt1)
	assert.Len(t, result.Frames, 0)

	// Last packet (seq=2 is missing)
	pkt3 := &BufferedPacket{
		SequenceNumber: 1002,
		Timestamp:      90000,
		Payload:        []byte{0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	}
	result = buffer.InsertPacket(pkt3)
	assert.Len(t, result.Frames, 0, "Frame should not complete with missing packet")

	// Missing packet arrives
	pkt2 := &BufferedPacket{
		SequenceNumber: 1001,
		Timestamp:      90000,
		Payload:        []byte{0x02},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  false,
		},
	}
	result = buffer.InsertPacket(pkt2)
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 3, "Frame should complete when missing packet arrives")
}

func TestVideoPacketBuffer_DuplicatePacket(t *testing.T) {
	// Duplicate packet should be ignored

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	pkt := &BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	}

	// First insertion
	result := buffer.InsertPacket(pkt)
	assert.Len(t, result.Frames, 0)

	// Duplicate insertion
	result = buffer.InsertPacket(pkt)
	assert.Len(t, result.Frames, 0, "Duplicate should not cause issues")
}

func TestVideoPacketBuffer_MultipleFrames(t *testing.T) {
	// Process multiple consecutive frames

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	// Frame 1: seq 1000-1001
	result := buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 1000,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	})
	assert.Len(t, result.Frames, 0)

	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 1001,
		Timestamp:      90000,
		Payload:        []byte{0x02},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	})
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 2, "Frame 1 should have 2 packets")

	// Frame 2: seq 1002-1003 (different timestamp)
	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 1002,
		Timestamp:      93000, // Different timestamp
		Payload:        []byte{0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	})
	assert.Len(t, result.Frames, 0)

	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 1003,
		Timestamp:      93000,
		Payload:        []byte{0x04},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	})
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 2, "Frame 2 should have 2 packets")
	assert.Equal(t, int64(1002), result.Frames[0][0].SequenceNumber)
	assert.Equal(t, int64(1003), result.Frames[0][1].SequenceNumber)
}

func TestVideoPacketBuffer_SequenceWrap(t *testing.T) {
	// Sequence number wrap around using unwrapped sequence numbers
	// The buffer receives already unwrapped sequence numbers (int64)
	// This simulates what happens when sequences 65534, 65535 wrap to 65536, 65537

	buffer, err := NewVideoPacketBuffer(512)
	require.NoError(t, err)

	// Using unwrapped sequence numbers that represent wrap-around
	// In a real scenario, the unwrapper would convert:
	// 65534 -> 65534, 65535 -> 65535, 0 -> 65536, 1 -> 65537

	// Packet at unwrapped seq 65534 (first)
	result := buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 65534,
		Timestamp:      90000,
		Payload:        []byte{0x01},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: true,
			IsLastPacketInFrame:  false,
		},
	})
	assert.Len(t, result.Frames, 0)

	// Packet at unwrapped seq 65535
	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 65535,
		Timestamp:      90000,
		Payload:        []byte{0x02},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  false,
		},
	})
	assert.Len(t, result.Frames, 0)

	// Packet at unwrapped seq 65536 (would be 0 in 16-bit, but unwrapped to 65536)
	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 65536,
		Timestamp:      90000,
		Payload:        []byte{0x03},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  false,
		},
	})
	assert.Len(t, result.Frames, 0)

	// Packet at unwrapped seq 65537 (last)
	result = buffer.InsertPacket(&BufferedPacket{
		SequenceNumber: 65537,
		Timestamp:      90000,
		Payload:        []byte{0x04},
		VideoHeader: &RTPVideoHeader{
			IsFirstPacketInFrame: false,
			IsLastPacketInFrame:  true,
		},
	})
	require.Len(t, result.Frames, 1, "Should return 1 frame")
	require.Len(t, result.Frames[0], 4, "Frame should complete across sequence wrap")
	assert.Equal(t, int64(65534), result.Frames[0][0].SequenceNumber)
	assert.Equal(t, int64(65535), result.Frames[0][1].SequenceNumber)
	assert.Equal(t, int64(65536), result.Frames[0][2].SequenceNumber)
	assert.Equal(t, int64(65537), result.Frames[0][3].SequenceNumber)
}

func TestVideoPacketBuffer_InvalidSize(t *testing.T) {
	// Buffer size must be power of 2 and within [64, 2048]
	_, err := NewVideoPacketBuffer(100) // Not power of 2
	assert.Error(t, err)

	_, err = NewVideoPacketBuffer(4096) // Exceeds max
	assert.Error(t, err)

	_, err = NewVideoPacketBuffer(32) // Below min (valid power of 2 but < 64)
	assert.Error(t, err)

	_, err = NewVideoPacketBuffer(16) // Below min
	assert.Error(t, err)

	_, err = NewVideoPacketBuffer(64) // Min valid
	assert.NoError(t, err)

	_, err = NewVideoPacketBuffer(512) // Valid power of 2
	assert.NoError(t, err)

	_, err = NewVideoPacketBuffer(2048) // Max valid
	assert.NoError(t, err)
}
