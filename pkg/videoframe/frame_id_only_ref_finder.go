// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

// FrameIdOnlyRefFinder resolves frame references using only picture ID.
// This is used when picture ID is available but no temporal layer information.
//
// Reference: libwebrtc modules/video_coding/rtp_frame_id_only_ref_finder.cc
//
// Algorithm:
// - Keyframe: NumReferences = 0
// - Delta frame: NumReferences = 1, references the previous frame (ID - 1)
// - Frame ID is derived from picture ID (with 15-bit wrap-around handling)
//
// Note: Unlike VP8RefFinder or SeqNumOnlyRefFinder, this finder does not
// maintain a stash or wait for keyframes. It simply sets references based
// on the assumption that frames arrive in order or the decoder can handle
// missing references. This matches libwebrtc's implementation.
type FrameIdOnlyRefFinder struct {
	// unwrapper handles picture ID wrap-around (15-bit values)
	unwrapper *pictureIDUnwrapper
}

// pictureIDUnwrapper handles 15-bit picture ID wrap-around.
// VP8 picture ID can be 7-bit (0-127) or 15-bit (0-32767).
// This unwrapper handles the 15-bit case (which encompasses 7-bit).
type pictureIDUnwrapper struct {
	lastUnwrapped int64
	initialized   bool
}

// pictureIDModulus is the modulus for 15-bit picture ID (2^15 = 32768)
const pictureIDModulus = 1 << 15

// Unwrap unwraps a picture ID, handling wrap-around.
// Reference: libwebrtc modules/video_coding/rtp_frame_id_only_ref_finder.cc:22
func (u *pictureIDUnwrapper) Unwrap(pictureID int32) int64 {
	if pictureID < 0 {
		return -1
	}

	// Mask to 15 bits
	pid := int64(pictureID & (pictureIDModulus - 1))

	if !u.initialized {
		u.initialized = true
		u.lastUnwrapped = pid
		return pid
	}

	// Calculate the difference, handling wrap-around
	lastWrapped := u.lastUnwrapped & (pictureIDModulus - 1)
	diff := pid - lastWrapped

	// Handle wrap-around: if diff is more than half the range, assume wrap
	if diff > pictureIDModulus/2 {
		diff -= pictureIDModulus
	} else if diff < -pictureIDModulus/2 {
		diff += pictureIDModulus
	}

	u.lastUnwrapped += diff
	return u.lastUnwrapped
}

// NewFrameIdOnlyRefFinder creates a new FrameIdOnlyRefFinder.
func NewFrameIdOnlyRefFinder() *FrameIdOnlyRefFinder {
	return &FrameIdOnlyRefFinder{
		unwrapper: &pictureIDUnwrapper{},
	}
}

// ManageFrame processes a frame and resolves its references.
// Reference: libwebrtc rtp_frame_id_only_ref_finder.cc ManageFrame()
func (f *FrameIdOnlyRefFinder) ManageFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	if frame == nil || header == nil {
		return nil
	}

	// If no picture ID, fall back to returning the frame without modification
	if header.PictureID == NoPictureID {
		return []*EncodedFrame{frame}
	}

	// Unwrap picture ID
	unwrappedPID := f.unwrapper.Unwrap(header.PictureID)
	if unwrappedPID < 0 {
		return nil
	}

	// Set frame ID from picture ID
	// Reference: libwebrtc rtp_frame_id_only_ref_finder.cc:22
	// frame->SetId(unwrapper_.Unwrap(frame_id & (kFrameIdLength - 1)));
	frame.ID = unwrappedPID

	// Reference: libwebrtc rtp_frame_id_only_ref_finder.cc:23-24
	// frame->num_references = frame->frame_type() == VideoFrameType::kVideoFrameKey ? 0 : 1;
	// frame->references[0] = frame->Id() - 1;
	if frame.FrameType == FrameTypeKey {
		frame.NumReferences = 0
	} else {
		frame.NumReferences = 1
		frame.References[0] = frame.ID - 1
	}

	return []*EncodedFrame{frame}
}

// ClearTo clears all state for frames with unwrapped picture ID less than the given value.
// For FrameIdOnlyRefFinder, the ID is the unwrapped picture ID.
func (f *FrameIdOnlyRefFinder) ClearTo(pictureID int64) {
	// For FrameIdOnlyRefFinder, we don't maintain a stash,
	// so this is a no-op. The unwrapper state is maintained.
}
