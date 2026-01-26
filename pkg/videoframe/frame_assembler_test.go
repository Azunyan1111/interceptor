// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoFrameAssembler_AssembleSinglePacket(t *testing.T) {
	// Single packet frame should create EncodedFrame with payload copied

	assembler := NewVideoFrameAssembler()
	require.NotNil(t, assembler)

	packets := []*BufferedPacket{
		{
			SequenceNumber: 1000,
			Timestamp:      90000,
			Payload:        []byte{0x01, 0x02, 0x03, 0x04},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: true,
				IsLastPacketInFrame:  true,
				FrameType:            FrameTypeDelta,
			},
		},
	}

	frame := assembler.AssembleFrame(packets)

	require.NotNil(t, frame)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, frame.Data)
	assert.Equal(t, uint32(90000), frame.Timestamp)
}

func TestVideoFrameAssembler_AssembleMultiplePackets(t *testing.T) {
	// Multiple packets should have payloads concatenated in sequence order

	assembler := NewVideoFrameAssembler()

	packets := []*BufferedPacket{
		{
			SequenceNumber: 1000,
			Timestamp:      90000,
			Payload:        []byte{0x01, 0x02},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: true,
				IsLastPacketInFrame:  false,
			},
		},
		{
			SequenceNumber: 1001,
			Timestamp:      90000,
			Payload:        []byte{0x03, 0x04},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: false,
				IsLastPacketInFrame:  false,
			},
		},
		{
			SequenceNumber: 1002,
			Timestamp:      90000,
			Payload:        []byte{0x05, 0x06},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: false,
				IsLastPacketInFrame:  true,
			},
		},
	}

	frame := assembler.AssembleFrame(packets)

	require.NotNil(t, frame)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, frame.Data)
}

func TestVideoFrameAssembler_PreservesMetadata(t *testing.T) {
	// Frame metadata should be correctly extracted from packets

	assembler := NewVideoFrameAssembler()

	packets := []*BufferedPacket{
		{
			SequenceNumber: 1000,
			Timestamp:      90000,
			Payload:        []byte{0x01},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: true,
				IsLastPacketInFrame:  false,
				FrameType:            FrameTypeKey,
				PictureID:            42,
				TemporalIdx:          1,
			},
		},
		{
			SequenceNumber: 1001,
			Timestamp:      90000,
			Payload:        []byte{0x02},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: false,
				IsLastPacketInFrame:  false,
			},
		},
		{
			SequenceNumber: 1002,
			Timestamp:      90000,
			Payload:        []byte{0x03},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: false,
				IsLastPacketInFrame:  true,
			},
		},
	}

	frame := assembler.AssembleFrame(packets)

	require.NotNil(t, frame)
	assert.Equal(t, uint32(90000), frame.Timestamp)
	assert.Equal(t, uint16(1000), frame.FirstSeqNum)
	assert.Equal(t, uint16(1002), frame.LastSeqNum)
}

func TestVideoFrameAssembler_KeyFrameDetection(t *testing.T) {
	// Frame type should be extracted from first packet's VideoHeader

	assembler := NewVideoFrameAssembler()

	tests := []struct {
		name          string
		frameType     FrameType
		expectedFrame FrameType
	}{
		{
			name:          "Key frame",
			frameType:     FrameTypeKey,
			expectedFrame: FrameTypeKey,
		},
		{
			name:          "Delta frame",
			frameType:     FrameTypeDelta,
			expectedFrame: FrameTypeDelta,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packets := []*BufferedPacket{
				{
					SequenceNumber: 1000,
					Timestamp:      90000,
					Payload:        []byte{0x01, 0x02, 0x03},
					VideoHeader: &RTPVideoHeader{
						IsFirstPacketInFrame: true,
						IsLastPacketInFrame:  true,
						FrameType:            tt.frameType,
					},
				},
			}

			frame := assembler.AssembleFrame(packets)

			require.NotNil(t, frame)
			assert.Equal(t, tt.expectedFrame, frame.FrameType)
		})
	}
}

func TestVideoFrameAssembler_EmptyPackets(t *testing.T) {
	// Empty packet slice should return nil

	assembler := NewVideoFrameAssembler()

	frame := assembler.AssembleFrame([]*BufferedPacket{})
	assert.Nil(t, frame)

	frame = assembler.AssembleFrame(nil)
	assert.Nil(t, frame)
}

func TestVideoFrameAssembler_FrameIDIncrement(t *testing.T) {
	// Each assembled frame should have unique incrementing ID

	assembler := NewVideoFrameAssembler()

	packets1 := []*BufferedPacket{
		{
			SequenceNumber: 1000,
			Timestamp:      90000,
			Payload:        []byte{0x01},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: true,
				IsLastPacketInFrame:  true,
			},
		},
	}

	packets2 := []*BufferedPacket{
		{
			SequenceNumber: 1001,
			Timestamp:      93000,
			Payload:        []byte{0x02},
			VideoHeader: &RTPVideoHeader{
				IsFirstPacketInFrame: true,
				IsLastPacketInFrame:  true,
			},
		},
	}

	frame1 := assembler.AssembleFrame(packets1)
	frame2 := assembler.AssembleFrame(packets2)

	require.NotNil(t, frame1)
	require.NotNil(t, frame2)
	assert.Equal(t, int64(0), frame1.ID)
	assert.Equal(t, int64(1), frame2.ID)
}
