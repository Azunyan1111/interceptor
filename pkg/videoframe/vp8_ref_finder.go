// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"sort"
)

// VP8RefFinder resolves frame references using VP8 temporal layer information.
// This is used when VP8 temporal layer information (TID, TL0PICIDX) is available.
//
// Reference: libwebrtc modules/video_coding/rtp_vp8_ref_finder.cc
//
// VP8 Temporal Scalability:
// - TID (Temporal ID): 0 = base layer, 1+ = enhancement layers
// - TL0PICIDX: Counter that increments for each base layer (TID=0) frame
// - Frames in higher temporal layers reference frames in lower layers
//
// Reference structure:
// - Keyframe (TID=0): No references, starts a new GOP
// - Base layer (TID=0) delta: References previous base layer frame
// - Enhancement layer: References frame(s) in same or lower temporal layers
type VP8RefFinder struct {
	// pictureIDUnwrapper handles picture ID wrap-around
	pictureIDUnwrapper *pictureIDUnwrapper

	// tl0Unwrapper handles TL0PICIDX wrap-around (8-bit)
	tl0Unwrapper *tl0PicIdxUnwrapper

	// layerInfo maps TL0PICIDX to the picture ID of each temporal layer
	// layerInfo[tl0PicIdx][tid] = pictureID + 1 (0 means not set)
	// We add 1 to distinguish between "not set" (0) and "PictureID = 0" (stored as 1)
	layerInfo map[int64][maxTemporalLayers]int64

	// lastPictureID is the last successfully processed picture ID (unwrapped)
	lastPictureID int64

	// gotInitialFrame indicates if we've received the first keyframe
	gotInitialFrame bool

	// stashedFrames holds frames waiting for their dependencies
	stashedFrames []*stashedVP8Frame
}

// stashedVP8Frame holds a VP8 frame along with its metadata for later processing.
// Note: unwrappedPID and unwrappedTL0 are computed at stash time to avoid
// re-unwrapping which would corrupt the unwrapper state.
type stashedVP8Frame struct {
	frame        *EncodedFrame
	header       *RTPVideoHeader
	unwrappedPID int64 // Unwrapped picture ID (computed at stash time)
	unwrappedTL0 int64 // Unwrapped TL0PICIDX (computed at stash time)
}

// tl0PicIdxUnwrapper handles 8-bit TL0PICIDX wrap-around.
type tl0PicIdxUnwrapper struct {
	lastUnwrapped int64
	initialized   bool
}

// tl0PicIdxModulus is the modulus for 8-bit TL0PICIDX (2^8 = 256)
const tl0PicIdxModulus = 1 << 8

// maxTemporalLayers is the maximum number of temporal layers supported
const maxTemporalLayers = 4

// Unwrap unwraps a TL0PICIDX, handling wrap-around.
func (u *tl0PicIdxUnwrapper) Unwrap(tl0PicIdx int16) int64 {
	if tl0PicIdx < 0 {
		return -1
	}

	idx := int64(tl0PicIdx & (tl0PicIdxModulus - 1))

	if !u.initialized {
		u.initialized = true
		u.lastUnwrapped = idx
		return idx
	}

	lastWrapped := u.lastUnwrapped & (tl0PicIdxModulus - 1)
	diff := idx - lastWrapped

	if diff > tl0PicIdxModulus/2 {
		diff -= tl0PicIdxModulus
	} else if diff < -tl0PicIdxModulus/2 {
		diff += tl0PicIdxModulus
	}

	u.lastUnwrapped += diff
	return u.lastUnwrapped
}

// NewVP8RefFinder creates a new VP8RefFinder.
func NewVP8RefFinder() *VP8RefFinder {
	return &VP8RefFinder{
		pictureIDUnwrapper: &pictureIDUnwrapper{},
		tl0Unwrapper:       &tl0PicIdxUnwrapper{},
		layerInfo:          make(map[int64][maxTemporalLayers]int64),
		lastPictureID:      -1,
		gotInitialFrame:    false,
		stashedFrames:      make([]*stashedVP8Frame, 0),
	}
}

// ManageFrame processes a frame and resolves its references.
// Reference: libwebrtc rtp_vp8_ref_finder.cc ManageFrameInternal()
func (f *VP8RefFinder) ManageFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	if frame == nil || header == nil {
		return nil
	}

	// Validate temporal layer information
	if header.TemporalIdx == NoTemporalIdx || header.TL0PicIdx == NoTL0PicIdx || header.PictureID == NoPictureID {
		// Fall back to simpler reference finder if temporal info is incomplete
		return f.fallbackManageFrame(frame, header)
	}

	// Unwrap picture ID and TL0PICIDX
	unwrappedPID := f.pictureIDUnwrapper.Unwrap(header.PictureID)
	unwrappedTL0 := f.tl0Unwrapper.Unwrap(header.TL0PicIdx)

	if unwrappedPID < 0 || unwrappedTL0 < 0 {
		return nil
	}

	// Set frame ID from picture ID
	frame.ID = unwrappedPID

	tid := int(header.TemporalIdx)
	if tid >= maxTemporalLayers {
		tid = maxTemporalLayers - 1
	}

	// Handle keyframe
	if frame.FrameType == FrameTypeKey && tid == 0 {
		return f.handleKeyframe(frame, header, unwrappedPID, unwrappedTL0, tid)
	}

	// Handle delta frame
	return f.handleDeltaFrame(frame, header, unwrappedPID, unwrappedTL0, tid)
}

// handleKeyframe processes a VP8 keyframe.
// Reference: libwebrtc rtp_vp8_ref_finder.cc keyframe handling
func (f *VP8RefFinder) handleKeyframe(frame *EncodedFrame, header *RTPVideoHeader, pid, tl0 int64, tid int) []*EncodedFrame {
	// Keyframes have no references
	frame.NumReferences = 0

	// Initialize layer info for this GOP
	f.initLayerInfo(tl0, pid)

	// Update state
	f.lastPictureID = pid
	f.gotInitialFrame = true

	// Clear old layer info and stashed frames
	f.clearOldState(tl0)

	// Return the keyframe and any stashed frames that can now be resolved
	result := []*EncodedFrame{frame}
	resolved := f.tryResolveStashedFrames()
	result = append(result, resolved...)

	return result
}

// handleDeltaFrame processes a VP8 delta frame.
// Reference: libwebrtc rtp_vp8_ref_finder.cc inter frame handling
func (f *VP8RefFinder) handleDeltaFrame(frame *EncodedFrame, header *RTPVideoHeader, pid, tl0 int64, tid int) []*EncodedFrame {
	// If we haven't received a keyframe yet, stash this frame
	if !f.gotInitialFrame {
		f.stashFrame(frame, header, pid, tl0)
		return nil
	}

	// Check if we have the layer info for the previous TL0PICIDX
	prevTL0 := tl0
	if tid == 0 {
		prevTL0 = tl0 - 1
	}

	// Get references based on temporal layer
	refs, ok := f.getReferences(pid, tl0, prevTL0, tid)
	if !ok {
		// Missing dependencies, stash the frame
		f.stashFrame(frame, header, pid, tl0)
		return nil
	}

	// Set references
	frame.NumReferences = len(refs)
	for i, ref := range refs {
		if i < 5 {
			frame.References[i] = ref
		}
	}

	// Update layer info
	f.updateLayerInfo(tl0, tid, pid)

	// Update state
	f.lastPictureID = pid

	// Return this frame and try to resolve stashed frames
	result := []*EncodedFrame{frame}
	resolved := f.tryResolveStashedFrames()
	result = append(result, resolved...)

	return result
}

// getReferences returns the frame references for a given frame.
// Reference: libwebrtc rtp_vp8_ref_finder.cc reference calculation
// Note: layerInfo stores pictureID + 1, so we subtract 1 when reading
func (f *VP8RefFinder) getReferences(pid, tl0, prevTL0 int64, tid int) ([]int64, bool) {
	var refs []int64

	if tid == 0 {
		// Base layer: reference previous base layer frame
		layerInfo, ok := f.layerInfo[prevTL0]
		if !ok || layerInfo[0] == 0 {
			return nil, false // Missing previous base layer
		}
		refs = append(refs, layerInfo[0]-1) // Subtract 1 to get actual pictureID
	} else {
		// Enhancement layer: reference frames in same or lower temporal layers
		// First, try to find reference in current TL0PICIDX
		layerInfo, hasCurrent := f.layerInfo[tl0]

		// Reference lower temporal layers from current TL0PICIDX
		if hasCurrent {
			for t := tid - 1; t >= 0; t-- {
				if layerInfo[t] != 0 {
					refs = append(refs, layerInfo[t]-1) // Subtract 1
					break                              // Only need one reference from lower layers
				}
			}
		}

		// If no references found from current TL0, check previous TL0PICIDX
		if len(refs) == 0 {
			prevLayerInfo, hasPrev := f.layerInfo[prevTL0]
			if hasPrev {
				for t := tid; t >= 0; t-- {
					if prevLayerInfo[t] != 0 {
						refs = append(refs, prevLayerInfo[t]-1) // Subtract 1
						break
					}
				}
			}
		}

		// Still no references? Try any available base layer
		if len(refs) == 0 {
			// Search through all known TL0PICIDXes for a base layer reference
			for _, info := range f.layerInfo {
				if info[0] != 0 {
					refs = append(refs, info[0]-1) // Subtract 1
					break
				}
			}
		}
	}

	if len(refs) == 0 {
		return nil, false
	}

	return refs, true
}

// initLayerInfo initializes layer info for a new GOP starting at the given TL0PICIDX.
// Note: We store pictureID + 1 to distinguish "not set" (0) from pictureID=0 (stored as 1)
func (f *VP8RefFinder) initLayerInfo(tl0, pid int64) {
	var info [maxTemporalLayers]int64
	info[0] = pid + 1 // TID=0 frame, store as pid+1
	f.layerInfo[tl0] = info
}

// updateLayerInfo updates the layer info for the given TL0PICIDX and temporal layer.
// Note: We store pictureID + 1 to distinguish "not set" (0) from pictureID=0 (stored as 1)
func (f *VP8RefFinder) updateLayerInfo(tl0 int64, tid int, pid int64) {
	info := f.layerInfo[tl0]
	info[tid] = pid + 1 // Store as pid+1
	f.layerInfo[tl0] = info
}

// clearOldState removes old layer info and stashed frames.
func (f *VP8RefFinder) clearOldState(currentTL0 int64) {
	// Keep only recent TL0PICIDX values
	const maxTL0History = 10
	for tl0 := range f.layerInfo {
		if currentTL0-tl0 > maxTL0History {
			delete(f.layerInfo, tl0)
		}
	}

	// Clear stashed frames that are too old (using pre-computed unwrapped PID)
	var remaining []*stashedVP8Frame
	for _, stashed := range f.stashedFrames {
		// Keep frames that are recent (compare using unwrapped PIDs)
		if f.lastPictureID-stashed.unwrappedPID < 100 {
			remaining = append(remaining, stashed)
		}
	}
	f.stashedFrames = remaining
}

// stashFrame adds a frame to the stash for later processing.
// unwrappedPID and unwrappedTL0 are passed in to avoid re-unwrapping.
func (f *VP8RefFinder) stashFrame(frame *EncodedFrame, header *RTPVideoHeader, unwrappedPID, unwrappedTL0 int64) {
	// Limit stash size
	if len(f.stashedFrames) >= maxStashedFrames {
		f.stashedFrames = f.stashedFrames[1:]
	}

	f.stashedFrames = append(f.stashedFrames, &stashedVP8Frame{
		frame:        frame,
		header:       header,
		unwrappedPID: unwrappedPID,
		unwrappedTL0: unwrappedTL0,
	})

	// Sort stashed frames by unwrapped picture ID (not raw PictureID)
	sort.Slice(f.stashedFrames, func(i, j int) bool {
		return f.stashedFrames[i].unwrappedPID < f.stashedFrames[j].unwrappedPID
	})
}

// tryResolveStashedFrames attempts to resolve stashed frames.
func (f *VP8RefFinder) tryResolveStashedFrames() []*EncodedFrame {
	var result []*EncodedFrame

	for {
		resolved := false

		for i := 0; i < len(f.stashedFrames); i++ {
			stashed := f.stashedFrames[i]

			// Try to process this frame
			frames := f.tryProcessStashedFrame(stashed)
			if len(frames) > 0 {
				result = append(result, frames...)

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

// tryProcessStashedFrame attempts to process a single stashed frame.
// Uses the pre-computed unwrapped values stored in the stashed frame.
func (f *VP8RefFinder) tryProcessStashedFrame(stashed *stashedVP8Frame) []*EncodedFrame {
	header := stashed.header
	frame := stashed.frame

	if header.TemporalIdx == NoTemporalIdx || header.TL0PicIdx == NoTL0PicIdx || header.PictureID == NoPictureID {
		return nil
	}

	// Use pre-computed unwrapped values (do NOT re-unwrap)
	unwrappedPID := stashed.unwrappedPID
	unwrappedTL0 := stashed.unwrappedTL0

	tid := int(header.TemporalIdx)
	if tid >= maxTemporalLayers {
		tid = maxTemporalLayers - 1
	}

	prevTL0 := unwrappedTL0
	if tid == 0 {
		prevTL0 = unwrappedTL0 - 1
	}

	refs, ok := f.getReferences(unwrappedPID, unwrappedTL0, prevTL0, tid)
	if !ok {
		return nil
	}

	// Set frame ID and references
	frame.ID = unwrappedPID
	frame.NumReferences = len(refs)
	for i, ref := range refs {
		if i < 5 {
			frame.References[i] = ref
		}
	}

	// Update layer info
	f.updateLayerInfo(unwrappedTL0, tid, unwrappedPID)

	// Update state
	f.lastPictureID = unwrappedPID

	return []*EncodedFrame{frame}
}

// fallbackManageFrame handles frames with incomplete temporal layer info.
func (f *VP8RefFinder) fallbackManageFrame(frame *EncodedFrame, header *RTPVideoHeader) []*EncodedFrame {
	// Use picture ID only if available
	if header.PictureID != NoPictureID {
		unwrappedPID := f.pictureIDUnwrapper.Unwrap(header.PictureID)
		frame.ID = unwrappedPID

		if frame.FrameType == FrameTypeKey {
			frame.NumReferences = 0
		} else {
			frame.NumReferences = 1
			frame.References[0] = frame.ID - 1
		}

		return []*EncodedFrame{frame}
	}

	// No temporal info and no picture ID - just set basic references
	if frame.FrameType == FrameTypeKey {
		frame.NumReferences = 0
	} else {
		frame.NumReferences = 1
		frame.References[0] = frame.ID - 1
	}

	return []*EncodedFrame{frame}
}

// ClearTo clears all state for frames with unwrapped picture ID less than the given value.
// For VP8RefFinder, the ID is the unwrapped picture ID.
func (f *VP8RefFinder) ClearTo(pictureID int64) {
	// Clear stashed frames before the given picture ID (using pre-computed unwrapped PID)
	var remaining []*stashedVP8Frame
	for _, stashed := range f.stashedFrames {
		// Use pre-computed unwrapped PID for comparison
		if stashed.unwrappedPID >= pictureID {
			remaining = append(remaining, stashed)
		}
	}
	f.stashedFrames = remaining
}
