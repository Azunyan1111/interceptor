// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"strings"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

// EncodedFramesKey is the Attributes key for accessing completed EncodedFrames.
// When frames are completed, they will be available via attrs.Get(EncodedFramesKey).
// The value is []*EncodedFrame (slice of frame pointers).
// Multiple frames may be returned when packet loss recovery completes multiple frames.
const EncodedFramesKey = "videoframe.EncodedFrames"

// EncodedFrameKey is the Attributes key for accessing the first completed EncodedFrame.
// Deprecated: Use EncodedFramesKey to handle multiple completed frames correctly.
// When a frame is completed, it will be available via attrs.Get(EncodedFrameKey).
const EncodedFrameKey = "videoframe.EncodedFrame"

// defaultPacketBufferSize is the default packet buffer size.
// Reference: libwebrtc kPacketBufferStartSize = 512
const defaultPacketBufferSize = 512

// ReceiverInterceptorFactory is a interceptor.Factory for ReceiverInterceptor.
type ReceiverInterceptorFactory struct {
	opts []ReceiverInterceptorOption
}

// NewReceiverInterceptor returns a new ReceiverInterceptorFactory.
func NewReceiverInterceptor(opts ...ReceiverInterceptorOption) (*ReceiverInterceptorFactory, error) {
	return &ReceiverInterceptorFactory{opts: opts}, nil
}

// NewInterceptor constructs a new ReceiverInterceptor.
func (f *ReceiverInterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	r := &ReceiverInterceptor{
		streams:          make(map[uint32]*streamState),
		packetBufferSize: defaultPacketBufferSize,
	}

	for _, opt := range f.opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}

	if r.loggerFactory == nil {
		r.loggerFactory = logging.NewDefaultLoggerFactory()
	}
	if r.log == nil {
		r.log = r.loggerFactory.NewLogger("videoframe")
	}

	return r, nil
}

// streamState holds per-stream state for video frame assembly.
type streamState struct {
	packetBuffer   *VideoPacketBuffer
	frameAssembler *VideoFrameAssembler
	seqUnwrapper   *sequenceUnwrapper

	// Each ref finder type is created lazily and kept for the stream lifetime.
	// Frame-level selection chooses the appropriate ref finder based on available info.
	seqNumOnlyRefFinder  *SeqNumOnlyRefFinder
	frameIdOnlyRefFinder *FrameIdOnlyRefFinder
	vp8RefFinder         *VP8RefFinder
}

// sequenceUnwrapper unwraps 16-bit sequence numbers to int64.
type sequenceUnwrapper struct {
	lastSeq  int64
	started  bool
}

func (u *sequenceUnwrapper) unwrap(seq uint16) int64 {
	if !u.started {
		u.lastSeq = int64(seq)
		u.started = true
		return u.lastSeq
	}

	// Calculate difference handling wrap-around
	diff := int64(seq) - (u.lastSeq & 0xFFFF)
	if diff > 32768 {
		diff -= 65536
	} else if diff < -32768 {
		diff += 65536
	}

	u.lastSeq += diff
	return u.lastSeq
}

// ReceiverInterceptor assembles video frames from RTP packets.
// Completed frames are made available via interceptor.Attributes.
//
// Usage:
//
//	frames, ok := attrs.Get(EncodedFramesKey).([]*EncodedFrame)
//	if ok && len(frames) > 0 {
//	    for _, frame := range frames {
//	        // Process frame
//	    }
//	}
//
// This interceptor:
// 1. Parses VP8 RTP payloads to detect frame boundaries
// 2. Buffers packets until a complete frame is available
// 3. Assembles complete frames and adds them to Attributes
//    - EncodedFramesKey: []*EncodedFrame (all completed frames)
//    - EncodedFrameKey: *EncodedFrame (first frame only, for backward compatibility)
//
// Reference: libwebrtc video/rtp_video_stream_receiver2.cc
type ReceiverInterceptor struct {
	interceptor.NoOp

	streams          map[uint32]*streamState
	streamsMu        sync.Mutex
	packetBufferSize uint16
	log              logging.LeveledLogger
	loggerFactory    logging.LoggerFactory
}

// BindRemoteStream lets you modify any incoming RTP packets.
// It is called once per RemoteStream.
func (r *ReceiverInterceptor) BindRemoteStream(
	info *interceptor.StreamInfo,
	reader interceptor.RTPReader,
) interceptor.RTPReader {
	// Only process VP8 streams
	if !isVP8Stream(info) {
		return reader
	}

	ssrc := info.SSRC

	// Initialize stream state
	r.streamsMu.Lock()
	state, err := r.getOrCreateStreamState(ssrc)
	r.streamsMu.Unlock()

	if err != nil {
		r.log.Warnf("Failed to create stream state for SSRC %d: %v", ssrc, err)
		return reader
	}

	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attrs, err := reader.Read(b, a)
		if err != nil {
			return n, attrs, err
		}

		// Parse RTP packet
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(b[:n]); err != nil {
			return n, attrs, nil // Pass through on parse error
		}

		// Parse VP8 payload
		vp8 := &codecs.VP8Packet{}
		if _, err := vp8.Unmarshal(pkt.Payload); err != nil {
			return n, attrs, nil // Pass through on VP8 parse error
		}

		// Create video header from VP8 packet
		videoHeader := NewRTPVideoHeaderFromVP8(vp8, pkt.Marker)

		// Unwrap sequence number
		unwrappedSeq := state.seqUnwrapper.unwrap(pkt.SequenceNumber)

		// Create buffered packet
		// Use vp8.Payload (depacketized video payload without VP8 descriptor)
		// instead of pkt.Payload (raw RTP payload with VP8 descriptor)
		// Reference: libwebrtc's depacketizer extracts video_payload from RTP payload
		//
		// IMPORTANT: Copy the payload because vp8.Payload references the Read buffer b,
		// which will be overwritten on the next Read call.
		// Reference: libwebrtc video_rtp_depacketizer copies video_payload
		payloadCopy := make([]byte, len(vp8.Payload))
		copy(payloadCopy, vp8.Payload)

		bufferedPkt := &BufferedPacket{
			SequenceNumber: unwrappedSeq,
			Timestamp:      pkt.Timestamp,
			Payload:        payloadCopy,
			VideoHeader:    videoHeader,
			MarkerBit:      pkt.Marker,
		}

		// Insert into buffer
		r.streamsMu.Lock()
		result := state.packetBuffer.InsertPacket(bufferedPkt)
		r.streamsMu.Unlock()

		// Check for completed frames
		if len(result.Frames) > 0 {
			var resolvedFrames []*EncodedFrame
			for _, framePackets := range result.Frames {
				frame := state.frameAssembler.AssembleFrame(framePackets)
				if frame == nil {
					continue
				}

				// Get video header from first packet for reference finder selection
				var firstHeader *RTPVideoHeader
				if len(framePackets) > 0 && framePackets[0].VideoHeader != nil {
					firstHeader = framePackets[0].VideoHeader
				}

				// Select appropriate reference finder based on frame's header info
				r.streamsMu.Lock()
				refFinder := r.selectRefFinderForFrame(state, firstHeader)

				// Resolve frame references
				resolved := refFinder.ManageFrame(frame, firstHeader)
				resolvedFrames = append(resolvedFrames, resolved...)
				r.streamsMu.Unlock()
			}

			if len(resolvedFrames) > 0 {
				if attrs == nil {
					attrs = make(interceptor.Attributes)
				}
				// Set both keys for compatibility
				attrs.Set(EncodedFramesKey, resolvedFrames)
				attrs.Set(EncodedFrameKey, resolvedFrames[0]) // First frame for backward compatibility
			}
		}

		return n, attrs, nil
	})
}

// UnbindRemoteStream is called when the Stream is removed.
func (r *ReceiverInterceptor) UnbindRemoteStream(info *interceptor.StreamInfo) {
	r.streamsMu.Lock()
	defer r.streamsMu.Unlock()
	delete(r.streams, info.SSRC)
}

// Close closes the interceptor.
func (r *ReceiverInterceptor) Close() error {
	r.streamsMu.Lock()
	defer r.streamsMu.Unlock()
	r.streams = make(map[uint32]*streamState)
	return nil
}

// getOrCreateStreamState gets or creates the stream state for the given SSRC.
func (r *ReceiverInterceptor) getOrCreateStreamState(ssrc uint32) (*streamState, error) {
	if state, ok := r.streams[ssrc]; ok {
		return state, nil
	}

	packetBuffer, err := NewVideoPacketBuffer(r.packetBufferSize)
	if err != nil {
		return nil, err
	}

	state := &streamState{
		packetBuffer:   packetBuffer,
		frameAssembler: NewVideoFrameAssembler(),
		seqUnwrapper:   &sequenceUnwrapper{},
	}

	r.streams[ssrc] = state
	return state, nil
}

// selectRefFinderForFrame selects the appropriate reference finder for a frame
// based on the available header information.
// This method should be called with streamsMu held.
//
// Reference finder selection (based on libwebrtc rtp_frame_reference_finder.cc):
// 1. If temporal layer info is available (TID, TL0PICIDX, PictureID all present) -> VP8RefFinder
// 2. If only picture ID is available -> FrameIdOnlyRefFinder
// 3. Otherwise -> SeqNumOnlyRefFinder
//
// Each ref finder is created lazily and kept for the stream lifetime.
// This allows frame-by-frame selection based on actual available info,
// avoiding the issue where a VP8RefFinder fallback behaves differently
// from SeqNumOnlyRefFinder.
func (r *ReceiverInterceptor) selectRefFinderForFrame(state *streamState, header *RTPVideoHeader) FrameReferenceFinder {
	refType := SelectRefFinderType(header)

	switch refType {
	case RefFinderVP8:
		if state.vp8RefFinder == nil {
			state.vp8RefFinder = NewVP8RefFinder()
		}
		return state.vp8RefFinder
	case RefFinderFrameIDOnly:
		if state.frameIdOnlyRefFinder == nil {
			state.frameIdOnlyRefFinder = NewFrameIdOnlyRefFinder()
		}
		return state.frameIdOnlyRefFinder
	default:
		if state.seqNumOnlyRefFinder == nil {
			state.seqNumOnlyRefFinder = NewSeqNumOnlyRefFinder()
		}
		return state.seqNumOnlyRefFinder
	}
}

// isVP8Stream checks if the stream is a VP8 video stream.
func isVP8Stream(info *interceptor.StreamInfo) bool {
	if info == nil {
		return false
	}
	return strings.EqualFold(info.MimeType, "video/VP8")
}
