// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeqNumOnlyRefFinder_Keyframe(t *testing.T) {
	// Keyframe should have no references
	finder := NewSeqNumOnlyRefFinder()

	frame := &EncodedFrame{
		ID:                   0, // Will be overwritten to FirstSeqNumUnwrapped
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}

	result := finder.ManageFrame(frame, nil)

	require.Len(t, result, 1)
	// ID is set to FirstSeqNumUnwrapped
	assert.Equal(t, int64(1000), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences, "Keyframe should have no references")
}

func TestSeqNumOnlyRefFinder_DeltaAfterKeyframe(t *testing.T) {
	// Delta frame after keyframe should reference the keyframe
	finder := NewSeqNumOnlyRefFinder()

	// First, send keyframe
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}
	result := finder.ManageFrame(keyframe, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1000), result[0].ID)

	// Then, send delta frame (first seq = 1001, which is last seq + 1 = 1000 + 1)
	delta := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1001,
		LastSeqNum:           1001,
		FirstSeqNumUnwrapped: 1001,
		LastSeqNumUnwrapped:  1001,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta, nil)

	require.Len(t, result, 1)
	// ID is set to FirstSeqNumUnwrapped
	assert.Equal(t, int64(1001), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences, "Delta frame should have 1 reference")
	// Reference is the FirstSeqNumUnwrapped of the previous frame (keyframe)
	assert.Equal(t, int64(1000), result[0].References[0], "Delta frame should reference keyframe's FirstSeqNumUnwrapped")
}

func TestSeqNumOnlyRefFinder_DeltaWithoutKeyframe(t *testing.T) {
	// Delta frame without prior keyframe should be stashed
	finder := NewSeqNumOnlyRefFinder()

	delta := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeDelta,
	}
	result := finder.ManageFrame(delta, nil)

	assert.Len(t, result, 0, "Delta frame without keyframe should be stashed")
}

func TestSeqNumOnlyRefFinder_ChainOfDeltaFrames(t *testing.T) {
	// Chain: keyframe -> delta1 -> delta2 -> delta3
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe (seq 1000)
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}
	result := finder.ManageFrame(keyframe, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1000), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences)

	// Delta 1 (seq 1001)
	delta1 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1001,
		LastSeqNum:           1001,
		FirstSeqNumUnwrapped: 1001,
		LastSeqNumUnwrapped:  1001,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta1, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1001), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(1000), result[0].References[0]) // References keyframe's FirstSeqNumUnwrapped

	// Delta 2 (seq 1002)
	delta2 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1002,
		LastSeqNum:           1002,
		FirstSeqNumUnwrapped: 1002,
		LastSeqNumUnwrapped:  1002,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta2, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1002), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(1001), result[0].References[0]) // References delta1's FirstSeqNumUnwrapped

	// Delta 3 (seq 1003)
	delta3 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1003,
		LastSeqNum:           1003,
		FirstSeqNumUnwrapped: 1003,
		LastSeqNumUnwrapped:  1003,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta3, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1003), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(1002), result[0].References[0]) // References delta2's FirstSeqNumUnwrapped
}

func TestSeqNumOnlyRefFinder_OutOfOrderFrames(t *testing.T) {
	// Frames arriving out of order should be stashed and resolved when possible
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe (seq 1000)
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}
	result := finder.ManageFrame(keyframe, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1000), result[0].ID)

	// Delta 2 arrives before Delta 1 (out of order) - seq 1002
	delta2 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1002,
		LastSeqNum:           1002,
		FirstSeqNumUnwrapped: 1002,
		LastSeqNumUnwrapped:  1002,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta2, nil)
	assert.Len(t, result, 0, "Delta 2 should be stashed (waiting for Delta 1)")

	// Delta 1 arrives (seq 1001)
	delta1 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1001,
		LastSeqNum:           1001,
		FirstSeqNumUnwrapped: 1001,
		LastSeqNumUnwrapped:  1001,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta1, nil)

	// Both Delta 1 and Delta 2 should be resolved
	require.Len(t, result, 2, "Both Delta 1 and stashed Delta 2 should be resolved")
	assert.Equal(t, int64(1001), result[0].ID)
	assert.Equal(t, int64(1000), result[0].References[0]) // References keyframe's FirstSeqNumUnwrapped
	assert.Equal(t, int64(1002), result[1].ID)
	assert.Equal(t, int64(1001), result[1].References[0]) // References delta1's FirstSeqNumUnwrapped
}

func TestSeqNumOnlyRefFinder_NewKeyframeClearsStash(t *testing.T) {
	// New keyframe should clear stashed frames from previous GOP
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe 1 (seq 1000)
	keyframe1 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}
	finder.ManageFrame(keyframe1, nil)

	// Delta (will be stashed because it's not continuous) - seq 1005 (expected 1001)
	delta := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1005,
		LastSeqNum:           1005,
		FirstSeqNumUnwrapped: 1005,
		LastSeqNumUnwrapped:  1005,
		FrameType:            FrameTypeDelta,
	}
	result := finder.ManageFrame(delta, nil)
	assert.Len(t, result, 0, "Delta should be stashed")

	// New keyframe arrives (starts new GOP) - seq 1010
	keyframe2 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1010,
		LastSeqNum:           1010,
		FirstSeqNumUnwrapped: 1010,
		LastSeqNumUnwrapped:  1010,
		FrameType:            FrameTypeKey,
	}
	result = finder.ManageFrame(keyframe2, nil)

	// Only the new keyframe should be returned (stashed delta from old GOP is discarded)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1010), result[0].ID)
}

func TestSeqNumOnlyRefFinder_NilFrame(t *testing.T) {
	finder := NewSeqNumOnlyRefFinder()
	result := finder.ManageFrame(nil, nil)
	assert.Nil(t, result)
}

func TestSeqNumOnlyRefFinder_MultiPacketFrame(t *testing.T) {
	// Frame spanning multiple packets
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe spanning seq 1000-1002
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1002,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1002,
		FrameType:            FrameTypeKey,
	}
	result := finder.ManageFrame(keyframe, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1000), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences)

	// Delta spanning seq 1003-1005 (first = 1003 = keyframe last + 1 = 1002 + 1)
	delta := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1003,
		LastSeqNum:           1005,
		FirstSeqNumUnwrapped: 1003,
		LastSeqNumUnwrapped:  1005,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(1003), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	// References keyframe's FirstSeqNumUnwrapped (not LastSeqNumUnwrapped)
	assert.Equal(t, int64(1000), result[0].References[0])
}

func TestSeqNumOnlyRefFinder_ClearTo(t *testing.T) {
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1000,
		LastSeqNum:           1000,
		FirstSeqNumUnwrapped: 1000,
		LastSeqNumUnwrapped:  1000,
		FrameType:            FrameTypeKey,
	}
	finder.ManageFrame(keyframe, nil)

	// Stash some frames (non-continuous)
	for i := int64(5); i < 10; i++ {
		delta := &EncodedFrame{
			ID:                   0,
			FirstSeqNum:          uint16(1000 + i),
			LastSeqNum:           uint16(1000 + i),
			FirstSeqNumUnwrapped: 1000 + i,
			LastSeqNumUnwrapped:  1000 + i,
			FrameType:            FrameTypeDelta,
		}
		finder.ManageFrame(delta, nil)
	}

	// Clear frames before seq 1007
	finder.ClearTo(1007)

	// Verify that frames before 1007 are cleared (internal state check via behavior)
	// Add a frame at 1007 - it should still be stashed since we're not continuous
	delta := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1007,
		LastSeqNum:           1007,
		FirstSeqNumUnwrapped: 1007,
		LastSeqNumUnwrapped:  1007,
		FrameType:            FrameTypeDelta,
	}
	result := finder.ManageFrame(delta, nil)
	assert.Len(t, result, 0, "Frame should be stashed (not continuous)")
}

func TestSeqNumOnlyRefFinder_SequenceWrapAround(t *testing.T) {
	// Test 16-bit sequence number wrap-around using unwrapped seq nums
	finder := NewSeqNumOnlyRefFinder()

	// Keyframe near wrap boundary (seq 65534, unwrapped 65534)
	keyframe := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          65534,
		LastSeqNum:           65534,
		FirstSeqNumUnwrapped: 65534,
		LastSeqNumUnwrapped:  65534,
		FrameType:            FrameTypeKey,
	}
	result := finder.ManageFrame(keyframe, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(65534), result[0].ID)

	// Delta at seq 65535 (unwrapped 65535)
	delta1 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          65535,
		LastSeqNum:           65535,
		FirstSeqNumUnwrapped: 65535,
		LastSeqNumUnwrapped:  65535,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta1, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(65535), result[0].ID)
	assert.Equal(t, int64(65534), result[0].References[0])

	// Delta at seq 0 (wrapped, unwrapped 65536)
	delta2 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          0,
		LastSeqNum:           0,
		FirstSeqNumUnwrapped: 65536,
		LastSeqNumUnwrapped:  65536,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta2, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(65536), result[0].ID)
	assert.Equal(t, int64(65535), result[0].References[0])

	// Delta at seq 1 (unwrapped 65537)
	delta3 := &EncodedFrame{
		ID:                   0,
		FirstSeqNum:          1,
		LastSeqNum:           1,
		FirstSeqNumUnwrapped: 65537,
		LastSeqNumUnwrapped:  65537,
		FrameType:            FrameTypeDelta,
	}
	result = finder.ManageFrame(delta3, nil)
	require.Len(t, result, 1)
	assert.Equal(t, int64(65537), result[0].ID)
	assert.Equal(t, int64(65536), result[0].References[0])
}
