// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

// FrameReferenceFinder resolves frame dependencies for video frames.
// This is similar to libwebrtc's RtpFrameReferenceFinder
// (modules/video_coding/rtp_frame_reference_finder.cc).
//
// The reference finder determines which frames a given frame depends on
// (references) for decoding. This information is essential for:
// - Proper frame ordering in jitter buffers
// - Determining decodability of frames
// - Implementing frame dropping strategies
type FrameReferenceFinder interface {
	// ManageFrame processes a frame and resolves its references.
	// Returns a slice of frames that are ready for decoding (references resolved).
	// The returned slice may contain:
	// - The input frame if its references are resolved
	// - Previously stashed frames that can now be resolved
	// - Empty slice if the frame needs to wait for dependencies
	//
	// Reference: libwebrtc rtp_frame_reference_finder.cc ManageFrame()
	ManageFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame

	// ClearTo clears all state for frames with ID less than the given value.
	// This is called when a keyframe is received to reset the reference finder state.
	//
	// The ID interpretation depends on the reference finder implementation:
	// - SeqNumOnlyRefFinder: unwrapped sequence number (FirstSeqNumUnwrapped)
	// - FrameIdOnlyRefFinder: unwrapped picture ID
	// - VP8RefFinder: unwrapped picture ID
	ClearTo(id int64)
}

// RefFinderType indicates the type of reference finder to use.
type RefFinderType int

const (
	// RefFinderSeqNumOnly uses only sequence numbers to determine references.
	// Used when no picture ID or temporal layer information is available.
	// Reference: libwebrtc rtp_seq_num_only_ref_finder.cc
	RefFinderSeqNumOnly RefFinderType = iota

	// RefFinderFrameIDOnly uses picture ID to determine references.
	// Used when picture ID is available but no temporal layer information.
	// Reference: libwebrtc rtp_frame_id_only_ref_finder.cc
	RefFinderFrameIDOnly

	// RefFinderVP8 uses VP8 temporal layer information to determine references.
	// Used when VP8 temporal layer information (TID, TL0PICIDX, PictureID) is all available.
	// Reference: libwebrtc rtp_vp8_ref_finder.cc
	RefFinderVP8
)

// SelectRefFinderType determines which reference finder type to use based on
// the available information in the RTPVideoHeader.
//
// Selection logic (based on libwebrtc rtp_frame_reference_finder.cc:56-114):
// 1. If temporal layer info (TemporalIdx, TL0PicIdx, PictureID all present) -> VP8RefFinder
// 2. If only PictureID is available -> FrameIdOnlyRefFinder
// 3. Otherwise -> SeqNumOnlyRefFinder
//
// Note: VP8RefFinder requires PictureID for frame ID assignment. Without PictureID,
// the frame ID space would be inconsistent with other ref finders.
func SelectRefFinderType(header *RTPVideoHeader) RefFinderType {
	if header == nil {
		return RefFinderSeqNumOnly
	}

	// Check if full temporal layer information is available (including PictureID)
	// Reference: libwebrtc rtp_frame_reference_finder.cc:66-82
	// VP8RefFinder requires PictureID for proper frame ID assignment
	hasFullTemporalInfo := header.TemporalIdx != NoTemporalIdx &&
		header.TL0PicIdx != NoTL0PicIdx &&
		header.PictureID != NoPictureID

	if hasFullTemporalInfo {
		return RefFinderVP8
	}

	// Check if picture ID is available
	if header.PictureID != NoPictureID {
		return RefFinderFrameIDOnly
	}

	return RefFinderSeqNumOnly
}

// maxStashedFrames is the maximum number of frames to stash while waiting for dependencies.
// Reference: libwebrtc kMaxStashedFrames (various ref finder implementations)
const maxStashedFrames = 100

// maxPaddingAge is the maximum age of padding packets to track.
// Reference: libwebrtc kMaxPaddingAge
const maxPaddingAge = 100
