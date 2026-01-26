// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"sync/atomic"
)

// EncodedFrame represents a complete video frame assembled from RTP packets.
// This structure is similar to libwebrtc's EncodedFrame (api/video/encoded_frame.h).
type EncodedFrame struct {
	// ID is the unique identifier for this frame (incrementing counter).
	ID int64

	// FirstSeqNum is the RTP sequence number of the first packet in this frame.
	FirstSeqNum uint16

	// LastSeqNum is the RTP sequence number of the last packet in this frame.
	LastSeqNum uint16

	// Timestamp is the RTP timestamp of the frame.
	Timestamp uint32

	// FrameType indicates whether this is a key frame or delta frame.
	FrameType FrameType

	// Data contains the assembled frame payload (concatenated packet payloads).
	Data []byte

	// Width is the frame width in pixels (if available).
	Width uint32

	// Height is the frame height in pixels (if available).
	Height uint32

	// NumReferences is the number of frames this frame references.
	NumReferences int

	// References contains frame IDs that this frame references.
	References [5]int64
}

// VideoFrameAssembler assembles complete video frames from packets.
// This is similar to libwebrtc's RtpFrameObject creation (video/rtp_video_stream_receiver2.cc:818-920).
// This struct is safe for concurrent use.
type VideoFrameAssembler struct {
	frameIDCounter atomic.Int64
}

// NewVideoFrameAssembler creates a new VideoFrameAssembler.
func NewVideoFrameAssembler() *VideoFrameAssembler {
	return &VideoFrameAssembler{}
}

// AssembleFrame assembles a complete frame from the given packets.
// Packets must be in sequence order and represent a complete frame.
// Returns nil if packets is empty or nil.
//
// Reference: libwebrtc video/rtp_video_stream_receiver2.cc:818-920
// This function:
// 1. Concatenates all packet payloads in sequence order
// 2. Extracts metadata from the first packet (timestamp, frame type, etc.)
// 3. Records sequence number range (first and last)
// 4. Assigns a unique frame ID
func (a *VideoFrameAssembler) AssembleFrame(packets []*BufferedPacket) *EncodedFrame {
	if len(packets) == 0 {
		return nil
	}

	// Calculate total payload size for pre-allocation
	totalSize := 0
	for _, pkt := range packets {
		totalSize += len(pkt.Payload)
	}

	// Concatenate payloads
	data := make([]byte, 0, totalSize)
	for _, pkt := range packets {
		data = append(data, pkt.Payload...)
	}

	// Extract metadata from first packet
	firstPkt := packets[0]
	lastPkt := packets[len(packets)-1]

	// Atomically get and increment frame ID
	frameID := a.frameIDCounter.Add(1) - 1

	frame := &EncodedFrame{
		ID:          frameID,
		FirstSeqNum: uint16(firstPkt.SequenceNumber & 0xFFFF),
		LastSeqNum:  uint16(lastPkt.SequenceNumber & 0xFFFF),
		Timestamp:   firstPkt.Timestamp,
		Data:        data,
	}

	// Extract frame type from first packet's VideoHeader
	if firstPkt.VideoHeader != nil {
		frame.FrameType = firstPkt.VideoHeader.FrameType
	}

	return frame
}
