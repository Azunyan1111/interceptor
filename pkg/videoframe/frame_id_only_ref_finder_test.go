// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrameIdOnlyRefFinder_Keyframe(t *testing.T) {
	finder := NewFrameIdOnlyRefFinder()

	frame := &EncodedFrame{
		ID:        0,
		FrameType: FrameTypeKey,
	}
	header := &RTPVideoHeader{
		PictureID: 100,
	}

	result := finder.ManageFrame(frame, header)

	require.Len(t, result, 1)
	assert.Equal(t, int64(100), result[0].ID, "Frame ID should be set from picture ID")
	assert.Equal(t, 0, result[0].NumReferences, "Keyframe should have no references")
}

func TestFrameIdOnlyRefFinder_DeltaFrame(t *testing.T) {
	finder := NewFrameIdOnlyRefFinder()

	// First, keyframe
	keyframe := &EncodedFrame{
		ID:        0,
		FrameType: FrameTypeKey,
	}
	keyHeader := &RTPVideoHeader{
		PictureID: 100,
	}
	finder.ManageFrame(keyframe, keyHeader)

	// Then, delta frame
	delta := &EncodedFrame{
		ID:        0,
		FrameType: FrameTypeDelta,
	}
	deltaHeader := &RTPVideoHeader{
		PictureID: 101,
	}

	result := finder.ManageFrame(delta, deltaHeader)

	require.Len(t, result, 1)
	assert.Equal(t, int64(101), result[0].ID, "Frame ID should be set from picture ID")
	assert.Equal(t, 1, result[0].NumReferences, "Delta frame should have 1 reference")
	assert.Equal(t, int64(100), result[0].References[0], "Delta frame should reference previous frame")
}

func TestFrameIdOnlyRefFinder_ChainOfFrames(t *testing.T) {
	finder := NewFrameIdOnlyRefFinder()

	// Keyframe with picture ID 0
	keyframe := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{PictureID: 0}
	result := finder.ManageFrame(keyframe, keyHeader)
	require.Len(t, result, 1)
	assert.Equal(t, int64(0), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences)

	// Delta frames 1, 2, 3
	for i := int32(1); i <= 3; i++ {
		delta := &EncodedFrame{FrameType: FrameTypeDelta}
		deltaHeader := &RTPVideoHeader{PictureID: i}
		result = finder.ManageFrame(delta, deltaHeader)

		require.Len(t, result, 1)
		assert.Equal(t, int64(i), result[0].ID)
		assert.Equal(t, 1, result[0].NumReferences)
		assert.Equal(t, int64(i-1), result[0].References[0])
	}
}

func TestFrameIdOnlyRefFinder_PictureIDWrapAround(t *testing.T) {
	// Test 15-bit picture ID wrap-around (32767 -> 0)
	finder := NewFrameIdOnlyRefFinder()

	// Frame at picture ID 32766
	frame1 := &EncodedFrame{FrameType: FrameTypeKey}
	header1 := &RTPVideoHeader{PictureID: 32766}
	result := finder.ManageFrame(frame1, header1)
	require.Len(t, result, 1)
	assert.Equal(t, int64(32766), result[0].ID)

	// Frame at picture ID 32767
	frame2 := &EncodedFrame{FrameType: FrameTypeDelta}
	header2 := &RTPVideoHeader{PictureID: 32767}
	result = finder.ManageFrame(frame2, header2)
	require.Len(t, result, 1)
	assert.Equal(t, int64(32767), result[0].ID)
	assert.Equal(t, int64(32766), result[0].References[0])

	// Frame at picture ID 0 (wrapped)
	frame3 := &EncodedFrame{FrameType: FrameTypeDelta}
	header3 := &RTPVideoHeader{PictureID: 0}
	result = finder.ManageFrame(frame3, header3)
	require.Len(t, result, 1)
	assert.Equal(t, int64(32768), result[0].ID, "Wrapped picture ID should unwrap correctly")
	assert.Equal(t, int64(32767), result[0].References[0])

	// Frame at picture ID 1 (after wrap)
	frame4 := &EncodedFrame{FrameType: FrameTypeDelta}
	header4 := &RTPVideoHeader{PictureID: 1}
	result = finder.ManageFrame(frame4, header4)
	require.Len(t, result, 1)
	assert.Equal(t, int64(32769), result[0].ID)
	assert.Equal(t, int64(32768), result[0].References[0])
}

func TestFrameIdOnlyRefFinder_7BitPictureID(t *testing.T) {
	// Test 7-bit picture ID (0-127)
	finder := NewFrameIdOnlyRefFinder()

	// Frame at picture ID 126
	frame1 := &EncodedFrame{FrameType: FrameTypeKey}
	header1 := &RTPVideoHeader{PictureID: 126}
	result := finder.ManageFrame(frame1, header1)
	require.Len(t, result, 1)
	assert.Equal(t, int64(126), result[0].ID)

	// Frame at picture ID 127
	frame2 := &EncodedFrame{FrameType: FrameTypeDelta}
	header2 := &RTPVideoHeader{PictureID: 127}
	result = finder.ManageFrame(frame2, header2)
	require.Len(t, result, 1)
	assert.Equal(t, int64(127), result[0].ID)

	// With 7-bit IDs used in a 15-bit unwrapper context,
	// the wrap happens at different points, but small deltas should work
}

func TestFrameIdOnlyRefFinder_NoPictureID(t *testing.T) {
	finder := NewFrameIdOnlyRefFinder()

	frame := &EncodedFrame{
		ID:        42,
		FrameType: FrameTypeDelta,
	}
	header := &RTPVideoHeader{
		PictureID: NoPictureID, // -1
	}

	result := finder.ManageFrame(frame, header)

	// Should return frame unmodified when no picture ID
	require.Len(t, result, 1)
	assert.Equal(t, int64(42), result[0].ID, "Frame ID should remain unchanged")
}

func TestFrameIdOnlyRefFinder_NilInputs(t *testing.T) {
	finder := NewFrameIdOnlyRefFinder()

	// Nil frame
	result := finder.ManageFrame(nil, &RTPVideoHeader{PictureID: 1})
	assert.Nil(t, result)

	// Nil header
	result = finder.ManageFrame(&EncodedFrame{}, nil)
	assert.Nil(t, result)

	// Both nil
	result = finder.ManageFrame(nil, nil)
	assert.Nil(t, result)
}

func TestPictureIDUnwrapper(t *testing.T) {
	tests := []struct {
		name     string
		sequence []int32
		expected []int64
	}{
		{
			name:     "Sequential",
			sequence: []int32{0, 1, 2, 3},
			expected: []int64{0, 1, 2, 3},
		},
		{
			name:     "Wrap at 15-bit boundary",
			sequence: []int32{32766, 32767, 0, 1},
			expected: []int64{32766, 32767, 32768, 32769},
		},
		{
			name:     "Small jump forward",
			sequence: []int32{100, 105, 110},
			expected: []int64{100, 105, 110},
		},
		{
			name:     "Small jump backward (reordering)",
			sequence: []int32{100, 98, 99, 101},
			expected: []int64{100, 98, 99, 101},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &pictureIDUnwrapper{}
			for i, pid := range tt.sequence {
				result := u.Unwrap(pid)
				assert.Equal(t, tt.expected[i], result, "Unwrap(%d) at index %d", pid, i)
			}
		})
	}
}

func TestPictureIDUnwrapper_NegativeInput(t *testing.T) {
	u := &pictureIDUnwrapper{}
	result := u.Unwrap(-1)
	assert.Equal(t, int64(-1), result, "Negative picture ID should return -1")
}
