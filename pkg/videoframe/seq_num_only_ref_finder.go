// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"sort"
)

// SeqNumOnlyRefFinder resolves frame references using only sequence numbers.
// This is used when no picture ID or temporal layer information is available.
//
// Reference: libwebrtc modules/video_coding/rtp_seq_num_only_ref_finder.cc
//
// Algorithm:
// - Keyframe: NumReferences = 0, starts a new GOP (Group of Pictures)
// - Delta frame: NumReferences = 1, references the previous frame
// - Frames are stashed if the previous frame hasn't been seen yet
// - When a keyframe arrives, all stashed frames before it are discarded
//
// Note: This implementation uses unwrapped sequence numbers (FirstSeqNumUnwrapped,
// LastSeqNumUnwrapped) for ordering and continuity checks to properly handle
// 16-bit sequence number wrap-around.
//
// ID Scheme: frame.ID is set to FirstSeqNumUnwrapped, and frame.References[0]
// also uses FirstSeqNumUnwrapped of the referenced frame for consistency.
type SeqNumOnlyRefFinder struct {
	// lastSeqNumGOP is the unwrapped sequence number of the first packet of the last keyframe.
	// This marks the start of the current GOP.
	lastSeqNumGOP int64

	// lastFrameFirstSeqNum is the unwrapped sequence number of the first packet of the
	// last successfully processed frame. Used as the frame ID for reference.
	lastFrameFirstSeqNum int64

	// lastFrameLastSeqNum is the unwrapped sequence number of the last packet of the
	// last successfully processed frame. Used for continuity detection.
	lastFrameLastSeqNum int64

	// gotInitialFrame indicates if we've received the first keyframe.
	gotInitialFrame bool

	// stashedFrames holds frames waiting for their dependencies.
	stashedFrames []*stashedSeqNumFrame
}

// stashedSeqNumFrame holds a frame along with its metadata for later processing.
type stashedSeqNumFrame struct {
	frame  *EncodedFrame
	header *RTPVideoHeader
}

// NewSeqNumOnlyRefFinder creates a new SeqNumOnlyRefFinder.
func NewSeqNumOnlyRefFinder() *SeqNumOnlyRefFinder {
	return &SeqNumOnlyRefFinder{
		lastSeqNumGOP:        -1,
		lastFrameFirstSeqNum: -1,
		lastFrameLastSeqNum:  -1,
		gotInitialFrame:      false,
		stashedFrames:        make([]*stashedSeqNumFrame, 0),
	}
}

// ManageFrame processes a frame and resolves its references.
// Reference: libwebrtc rtp_seq_num_only_ref_finder.cc ManageFrameInternal()
func (f *SeqNumOnlyRefFinder) ManageFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	if frame == nil {
		return nil
	}

	// Handle keyframe
	if frame.FrameType == FrameTypeKey {
		return f.handleKeyframe(frame, header)
	}

	// Handle delta frame
	return f.handleDeltaFrame(frame, header)
}

// handleKeyframe processes a keyframe.
// Reference: libwebrtc rtp_seq_num_only_ref_finder.cc lines handling keyframes
func (f *SeqNumOnlyRefFinder) handleKeyframe(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	// Set frame ID to FirstSeqNumUnwrapped for consistent ID scheme
	// This ensures frame.ID and frame.References use the same ID space
	frame.ID = frame.FirstSeqNumUnwrapped

	// Keyframes have no references
	frame.NumReferences = 0

	// Update GOP state using unwrapped sequence numbers
	f.lastSeqNumGOP = frame.FirstSeqNumUnwrapped
	f.lastFrameFirstSeqNum = frame.FirstSeqNumUnwrapped
	f.lastFrameLastSeqNum = frame.LastSeqNumUnwrapped
	f.gotInitialFrame = true

	// Clear stashed frames that are older than this keyframe
	f.clearStashedFramesBefore(frame.FirstSeqNumUnwrapped)

	// Return the keyframe and any stashed frames that can now be resolved
	result := []*EncodedFrame{frame}

	// Try to resolve stashed frames
	resolved := f.tryResolveStashedFrames()
	result = append(result, resolved...)

	return result
}

// handleDeltaFrame processes a delta frame.
// Reference: libwebrtc rtp_seq_num_only_ref_finder.cc lines handling inter frames
func (f *SeqNumOnlyRefFinder) handleDeltaFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	// If we haven't received a keyframe yet, stash this frame
	if !f.gotInitialFrame {
		f.stashFrame(frame, header)
		return nil
	}

	// Check if this frame is continuous with the last processed frame.
	// A frame is continuous if its first packet immediately follows the last packet
	// of the previous frame (using unwrapped sequence numbers).
	expectedFirstSeqNum := f.lastFrameLastSeqNum + 1

	if frame.FirstSeqNumUnwrapped == expectedFirstSeqNum {
		// Set frame ID to FirstSeqNumUnwrapped for consistent ID scheme
		frame.ID = frame.FirstSeqNumUnwrapped

		// Frame is continuous, set reference to previous frame
		// Reference uses FirstSeqNumUnwrapped of the previous frame
		// which equals (lastFrameLastSeqNum + 1) - (lastFrame packet count)
		// For simplicity, we track lastFrameFirstSeqNum separately
		frame.NumReferences = 1
		frame.References[0] = f.lastFrameFirstSeqNum

		// Update state
		f.lastFrameFirstSeqNum = frame.FirstSeqNumUnwrapped
		f.lastFrameLastSeqNum = frame.LastSeqNumUnwrapped

		// Return this frame and try to resolve stashed frames
		result := []*EncodedFrame{frame}
		resolved := f.tryResolveStashedFrames()
		result = append(result, resolved...)

		return result
	}

	// Frame is not continuous, check if it's before the current GOP
	if frame.FirstSeqNumUnwrapped < f.lastSeqNumGOP {
		// Frame is from a previous GOP, discard it
		return nil
	}

	// Frame is not continuous, stash it for later
	f.stashFrame(frame, header)
	return nil
}

// stashFrame adds a frame to the stash for later processing.
func (f *SeqNumOnlyRefFinder) stashFrame(frame *EncodedFrame, header *RTPVideoHeader) {
	// Limit stash size
	if len(f.stashedFrames) >= maxStashedFrames {
		// Remove oldest frame
		f.stashedFrames = f.stashedFrames[1:]
	}

	f.stashedFrames = append(f.stashedFrames, &stashedSeqNumFrame{
		frame:  frame,
		header: header,
	})

	// Sort stashed frames by unwrapped first sequence number
	sort.Slice(f.stashedFrames, func(i, j int) bool {
		return f.stashedFrames[i].frame.FirstSeqNumUnwrapped < f.stashedFrames[j].frame.FirstSeqNumUnwrapped
	})
}

// tryResolveStashedFrames attempts to resolve stashed frames.
// Returns frames that can now be processed.
func (f *SeqNumOnlyRefFinder) tryResolveStashedFrames() []*EncodedFrame {
	var result []*EncodedFrame

	for {
		resolved := false

		for i := 0; i < len(f.stashedFrames); i++ {
			stashed := f.stashedFrames[i]
			expectedFirstSeqNum := f.lastFrameLastSeqNum + 1

			if stashed.frame.FirstSeqNumUnwrapped == expectedFirstSeqNum {
				// Set frame ID to FirstSeqNumUnwrapped for consistent ID scheme
				stashed.frame.ID = stashed.frame.FirstSeqNumUnwrapped

				// This frame can be resolved - reference uses FirstSeqNumUnwrapped
				stashed.frame.NumReferences = 1
				stashed.frame.References[0] = f.lastFrameFirstSeqNum

				// Update state
				f.lastFrameFirstSeqNum = stashed.frame.FirstSeqNumUnwrapped
				f.lastFrameLastSeqNum = stashed.frame.LastSeqNumUnwrapped

				// Add to result
				result = append(result, stashed.frame)

				// Remove from stash
				f.stashedFrames = append(f.stashedFrames[:i], f.stashedFrames[i+1:]...)

				resolved = true
				break
			}
		}

		if !resolved {
			break
		}
	}

	return result
}

// clearStashedFramesBefore removes stashed frames with first sequence number before the given value.
func (f *SeqNumOnlyRefFinder) clearStashedFramesBefore(seqNum int64) {
	var remaining []*stashedSeqNumFrame
	for _, stashed := range f.stashedFrames {
		// Keep frames that are at or after the new GOP start (using unwrapped seq num)
		if stashed.frame.FirstSeqNumUnwrapped >= seqNum {
			remaining = append(remaining, stashed)
		}
	}
	f.stashedFrames = remaining
}

// ClearTo clears all state for frames with unwrapped sequence number less than the given value.
// For SeqNumOnlyRefFinder, the ID is FirstSeqNumUnwrapped.
func (f *SeqNumOnlyRefFinder) ClearTo(seqNum int64) {
	f.clearStashedFramesBefore(seqNum)
}
