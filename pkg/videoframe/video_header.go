// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package videoframe provides video frame assembly from RTP packets.
// It implements frame boundary detection and frame assembly similar to
// libwebrtc's PacketBuffer and RtpFrameObject.
package videoframe

import (
	"github.com/pion/rtp/codecs"
)

// FrameType indicates the type of video frame.
type FrameType int

const (
	// FrameTypeKey indicates a key frame (I-frame).
	FrameTypeKey FrameType = iota
	// FrameTypeDelta indicates a delta frame (P-frame or B-frame).
	FrameTypeDelta
)

// NoPictureID indicates that PictureID is not present.
const NoPictureID int32 = -1

// NoTemporalIdx indicates that TemporalIdx is not present.
const NoTemporalIdx int8 = -1

// NoTL0PicIdx indicates that TL0PicIdx is not present.
const NoTL0PicIdx int16 = -1

// RTPVideoHeader contains video-specific metadata extracted from RTP packets.
// This structure is similar to libwebrtc's RTPVideoHeader.
type RTPVideoHeader struct {
	// FrameType indicates whether this is a key frame or delta frame.
	FrameType FrameType

	// IsFirstPacketInFrame indicates if this packet is the first packet of a frame.
	// For VP8: S=1 && PID=0
	// For VP9: B=1
	IsFirstPacketInFrame bool

	// IsLastPacketInFrame indicates if this packet is the last packet of a frame.
	// Typically determined by the RTP marker bit or codec-specific flags.
	IsLastPacketInFrame bool

	// PictureID is the picture identifier. -1 if not present.
	PictureID int32

	// TemporalIdx is the temporal layer index. -1 if not present.
	TemporalIdx int8

	// TL0PicIdx is the temporal layer 0 picture index. -1 if not present.
	TL0PicIdx int16
}

// NewRTPVideoHeaderFromVP8 creates an RTPVideoHeader from a VP8 packet.
// Reference: RFC 7741 - RTP Payload Format for VP8 Video
// Reference: libwebrtc video_rtp_depacketizer_vp8.cc:175-176
func NewRTPVideoHeaderFromVP8(pkt *codecs.VP8Packet, marker bool) *RTPVideoHeader {
	header := &RTPVideoHeader{
		PictureID:   NoPictureID,
		TemporalIdx: NoTemporalIdx,
		TL0PicIdx:   NoTL0PicIdx,
	}

	// RFC 7741: First packet of frame has S=1 (start of partition) and PID=0 (partition index 0)
	// libwebrtc: video_header->is_first_packet_in_frame =
	//            vp8_header.beginningOfPartition && vp8_header.partitionId == 0;
	header.IsFirstPacketInFrame = pkt.S == 1 && pkt.PID == 0

	// Last packet is determined by RTP marker bit
	header.IsLastPacketInFrame = marker

	// Detect keyframe from VP8 payload header (only for first packet of frame)
	// Reference: RFC 7741 Section 4.3, libwebrtc video_rtp_depacketizer_vp8.cc:186-187
	// The P bit (bit 0 of first byte) indicates: 0 = keyframe, 1 = interframe
	if header.IsFirstPacketInFrame {
		header.FrameType = DetectVP8FrameType(pkt.Payload)
	}

	// Extract PictureID if present (I bit in extension)
	// RFC 7741: I=1 indicates PictureID is present
	if pkt.I == 1 {
		header.PictureID = int32(pkt.PictureID)
	}

	// Extract TemporalIdx if present (T bit in extension)
	// RFC 7741: T=1 indicates TID/Y/KEYIDX are present
	if pkt.T == 1 {
		header.TemporalIdx = int8(pkt.TID)
	}

	// Extract TL0PicIdx if present (L bit in extension)
	// RFC 7741: L=1 indicates TL0PICIDX is present
	if pkt.L == 1 {
		header.TL0PicIdx = int16(pkt.TL0PICIDX)
	}

	return header
}

// DetectVP8FrameType detects the frame type from the VP8 payload header.
// Reference: RFC 7741 Section 4.3 - VP8 Payload Header
//
// VP8 Payload Header (first byte after VP8 Payload Descriptor):
//
//	   0 1 2 3 4 5 6 7
//	  +-+-+-+-+-+-+-+-+
//	  |Size0|H| VER |P|
//	  +-+-+-+-+-+-+-+-+
//
// P bit (bit 0): 0 = keyframe, 1 = interframe (predictive frame)
//
// Reference: libwebrtc video_rtp_depacketizer_vp8.cc:186-187
//
//	if (video_header->is_first_packet_in_frame && (*vp8_payload & 0x01) == 0) {
//	    video_header->frame_type = VideoFrameType::kVideoFrameKey;
func DetectVP8FrameType(vp8Payload []byte) FrameType {
	if len(vp8Payload) == 0 {
		return FrameTypeDelta
	}
	// P bit is bit 0: 0 = keyframe, 1 = interframe
	if (vp8Payload[0] & 0x01) == 0 {
		return FrameTypeKey
	}
	return FrameTypeDelta
}
