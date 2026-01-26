// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"fmt"
)

// BufferedPacket represents an RTP packet stored in the VideoPacketBuffer.
// This structure is similar to libwebrtc's PacketBuffer::Packet.
type BufferedPacket struct {
	// SequenceNumber is the unwrapped sequence number (int64 to handle wrap-around).
	SequenceNumber int64

	// Timestamp is the RTP timestamp.
	Timestamp uint32

	// Payload is the video payload data.
	Payload []byte

	// VideoHeader contains video-specific metadata.
	VideoHeader *RTPVideoHeader

	// Continuous indicates if this packet is continuous with previous packets.
	// This is used internally by the buffer for frame completion detection.
	Continuous bool

	// MarkerBit is the RTP marker bit.
	MarkerBit bool
}

// InsertResult contains the result of inserting a packet into the buffer.
type InsertResult struct {
	// Frames contains completed frames, each as a slice of packets in sequence order.
	// Empty if no frame was completed.
	// Multiple frames may be returned when packet loss recovery completes multiple frames.
	Frames [][]*BufferedPacket
}

// VideoPacketBuffer buffers video RTP packets and detects complete frames.
// This is similar to libwebrtc's PacketBuffer (modules/video_coding/packet_buffer.cc).
//
// The buffer stores packets and detects when a complete frame is available
// by checking:
// 1. Packet continuity (no missing packets)
// 2. Frame boundaries (is_first_packet_in_frame and is_last_packet_in_frame)
// 3. Timestamp consistency (all packets in a frame have the same timestamp)
type VideoPacketBuffer struct {
	buffer []*BufferedPacket
	size   uint16
}

// NewVideoPacketBuffer creates a new VideoPacketBuffer with the specified size.
// Size must be a power of 2, between 64 and 2048 (inclusive).
// Reference: libwebrtc packet_buffer.cc constants:
// - kPacketBufferStartSize = 512
// - kPacketBufferMaxSize = 2048
func NewVideoPacketBuffer(size uint16) (*VideoPacketBuffer, error) {
	// Validate size is power of 2 and within allowed range [64, 2048]
	allowedSizes := []uint16{64, 128, 256, 512, 1024, 2048}
	if size == 0 || (size&(size-1)) != 0 || size < 64 || size > 2048 {
		return nil, fmt.Errorf("invalid buffer size %d: must be power of 2 (allowed: %v)", size, allowedSizes)
	}

	return &VideoPacketBuffer{
		buffer: make([]*BufferedPacket, size),
		size:   size,
	}, nil
}

// InsertPacket inserts a packet into the buffer and returns completed frames.
// Reference: libwebrtc PacketBuffer::InsertPacket (packet_buffer.cc:66-127)
func (b *VideoPacketBuffer) InsertPacket(pkt *BufferedPacket) InsertResult {
	result := InsertResult{}

	seqNum := pkt.SequenceNumber
	index := b.seqNumToIndex(seqNum)

	// Check for duplicate packet
	if b.buffer[index] != nil && b.buffer[index].SequenceNumber == seqNum {
		return result // Duplicate, ignore
	}

	// Clear slot if it contains an old packet
	if b.buffer[index] != nil {
		b.buffer[index] = nil
	}

	// Store packet
	pkt.Continuous = false
	b.buffer[index] = pkt

	// Try to find completed frames
	result.Frames = b.findFrames(seqNum)

	return result
}

// seqNumToIndex converts a sequence number to a buffer index.
func (b *VideoPacketBuffer) seqNumToIndex(seqNum int64) int {
	// Handle negative sequence numbers for wrap-around
	idx := seqNum % int64(b.size)
	if idx < 0 {
		idx += int64(b.size)
	}
	return int(idx)
}

// findFrames searches for completed frames starting from the given sequence number.
// Returns a slice of frames, where each frame is a slice of packets in sequence order.
// Reference: libwebrtc PacketBuffer::FindFrames (packet_buffer.cc:239-411)
func (b *VideoPacketBuffer) findFrames(seqNum int64) [][]*BufferedPacket {
	var result [][]*BufferedPacket

	// Scan backwards and forwards from the inserted packet to find continuous regions
	// and completed frames

	// First, mark packets as continuous where possible
	for i := int64(0); i < int64(b.size); i++ {
		currentSeq := seqNum - int64(b.size)/2 + i
		if !b.potentialNewFrame(currentSeq) {
			continue
		}

		index := b.seqNumToIndex(currentSeq)
		if b.buffer[index] == nil {
			continue
		}

		b.buffer[index].Continuous = true

		// Check if this completes a frame
		if b.buffer[index].VideoHeader != nil && b.buffer[index].VideoHeader.IsLastPacketInFrame {
			// Found end of frame, search backwards for start
			frame := b.extractFrame(currentSeq)
			if len(frame) > 0 {
				// Append as a separate frame (not flattened)
				result = append(result, frame)
				// Clear extracted packets from buffer
				for _, pkt := range frame {
					idx := b.seqNumToIndex(pkt.SequenceNumber)
					b.buffer[idx] = nil
				}
			}
		}
	}

	return result
}

// potentialNewFrame checks if a packet at the given sequence number could be part of a new frame.
// Reference: libwebrtc PacketBuffer::PotentialNewFrame (packet_buffer.cc:196-237)
func (b *VideoPacketBuffer) potentialNewFrame(seqNum int64) bool {
	index := b.seqNumToIndex(seqNum)
	pkt := b.buffer[index]

	if pkt == nil {
		return false
	}

	if pkt.SequenceNumber != seqNum {
		return false // Different packet at this index
	}

	// First packet in frame is always a potential new frame
	if pkt.VideoHeader != nil && pkt.VideoHeader.IsFirstPacketInFrame {
		return true
	}

	// Check previous packet for continuity
	prevIndex := b.seqNumToIndex(seqNum - 1)
	prevPkt := b.buffer[prevIndex]

	if prevPkt == nil {
		return false
	}

	if prevPkt.SequenceNumber != seqNum-1 {
		return false // Sequence gap
	}

	// Same timestamp required for same frame
	if prevPkt.Timestamp != pkt.Timestamp {
		return false
	}

	// Previous packet must be continuous
	return prevPkt.Continuous
}

// extractFrame extracts a complete frame ending at endSeqNum.
// Returns packets in sequence order, or empty if frame is incomplete.
func (b *VideoPacketBuffer) extractFrame(endSeqNum int64) []*BufferedPacket {
	// Find the start of the frame by walking backwards
	startSeqNum := endSeqNum

	for {
		index := b.seqNumToIndex(startSeqNum)
		pkt := b.buffer[index]

		if pkt == nil || pkt.SequenceNumber != startSeqNum {
			return nil // Missing packet
		}

		if pkt.VideoHeader != nil && pkt.VideoHeader.IsFirstPacketInFrame {
			break // Found start of frame
		}

		startSeqNum--

		// Prevent infinite loop
		if endSeqNum-startSeqNum > int64(b.size) {
			return nil
		}
	}

	// Verify all packets are present and continuous
	var packets []*BufferedPacket
	for seq := startSeqNum; seq <= endSeqNum; seq++ {
		index := b.seqNumToIndex(seq)
		pkt := b.buffer[index]

		if pkt == nil || pkt.SequenceNumber != seq {
			return nil // Missing packet
		}

		if !pkt.Continuous {
			return nil // Not continuous
		}

		packets = append(packets, pkt)
	}

	return packets
}
