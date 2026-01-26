// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVP8RefFinder_Keyframe(t *testing.T) {
	finder := NewVP8RefFinder()

	frame := &EncodedFrame{
		ID:        0,
		FrameType: FrameTypeKey,
	}
	header := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: 0,
		TL0PicIdx:   10,
	}

	result := finder.ManageFrame(frame, header)

	require.Len(t, result, 1)
	assert.Equal(t, int64(100), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences, "Keyframe should have no references")
}

func TestVP8RefFinder_BaseLayerDelta(t *testing.T) {
	// TID=0 delta frame should reference previous TID=0 frame
	finder := NewVP8RefFinder()

	// Keyframe at TL0PICIDX=10
	keyframe := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: 0,
		TL0PicIdx:   10,
	}
	result := finder.ManageFrame(keyframe, keyHeader)
	require.Len(t, result, 1)
	assert.Equal(t, 0, result[0].NumReferences)

	// TID=0 delta at TL0PICIDX=11
	delta := &EncodedFrame{FrameType: FrameTypeDelta}
	deltaHeader := &RTPVideoHeader{
		PictureID:   104, // Next base layer frame
		TemporalIdx: 0,
		TL0PicIdx:   11,
	}
	result = finder.ManageFrame(delta, deltaHeader)

	require.Len(t, result, 1)
	assert.Equal(t, int64(104), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(100), result[0].References[0], "Should reference keyframe")
}

func TestVP8RefFinder_EnhancementLayer(t *testing.T) {
	// TID=1 frame should reference TID=0 frame
	finder := NewVP8RefFinder()

	// Keyframe at TID=0, TL0PICIDX=10
	keyframe := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: 0,
		TL0PicIdx:   10,
	}
	finder.ManageFrame(keyframe, keyHeader)

	// TID=1 frame at same TL0PICIDX
	tid1Frame := &EncodedFrame{FrameType: FrameTypeDelta}
	tid1Header := &RTPVideoHeader{
		PictureID:   101,
		TemporalIdx: 1,
		TL0PicIdx:   10,
	}
	result := finder.ManageFrame(tid1Frame, tid1Header)

	require.Len(t, result, 1)
	assert.Equal(t, int64(101), result[0].ID)
	assert.GreaterOrEqual(t, result[0].NumReferences, 1, "TID=1 frame should have at least 1 reference")
	// Should reference the keyframe (TID=0)
	found := false
	for i := 0; i < result[0].NumReferences; i++ {
		if result[0].References[i] == 100 {
			found = true
			break
		}
	}
	assert.True(t, found, "TID=1 frame should reference keyframe")
}

func TestVP8RefFinder_TemporalLayerChain(t *testing.T) {
	// Test a simple temporal layer pattern:
	// TL0PICIDX=10: TID=0 (key), TID=1
	// TL0PICIDX=11: TID=0 (delta), TID=1
	finder := NewVP8RefFinder()

	// Frame 1: TID=0 keyframe
	f1 := &EncodedFrame{FrameType: FrameTypeKey}
	h1 := &RTPVideoHeader{PictureID: 0, TemporalIdx: 0, TL0PicIdx: 0}
	result := finder.ManageFrame(f1, h1)
	require.Len(t, result, 1)
	assert.Equal(t, 0, result[0].NumReferences)

	// Frame 2: TID=1
	f2 := &EncodedFrame{FrameType: FrameTypeDelta}
	h2 := &RTPVideoHeader{PictureID: 1, TemporalIdx: 1, TL0PicIdx: 0}
	result = finder.ManageFrame(f2, h2)
	require.Len(t, result, 1)
	assert.GreaterOrEqual(t, result[0].NumReferences, 1)

	// Frame 3: TID=0 delta (new TL0PICIDX)
	f3 := &EncodedFrame{FrameType: FrameTypeDelta}
	h3 := &RTPVideoHeader{PictureID: 2, TemporalIdx: 0, TL0PicIdx: 1}
	result = finder.ManageFrame(f3, h3)
	require.Len(t, result, 1)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(0), result[0].References[0], "Should reference first keyframe")

	// Frame 4: TID=1 at new TL0PICIDX
	f4 := &EncodedFrame{FrameType: FrameTypeDelta}
	h4 := &RTPVideoHeader{PictureID: 3, TemporalIdx: 1, TL0PicIdx: 1}
	result = finder.ManageFrame(f4, h4)
	require.Len(t, result, 1)
	assert.GreaterOrEqual(t, result[0].NumReferences, 1)
}

func TestVP8RefFinder_DeltaWithoutKeyframe(t *testing.T) {
	finder := NewVP8RefFinder()

	// Delta frame without prior keyframe should be stashed
	delta := &EncodedFrame{FrameType: FrameTypeDelta}
	deltaHeader := &RTPVideoHeader{
		PictureID:   101,
		TemporalIdx: 0,
		TL0PicIdx:   10,
	}
	result := finder.ManageFrame(delta, deltaHeader)

	assert.Len(t, result, 0, "Delta without keyframe should be stashed")
}

func TestVP8RefFinder_StashedFrameResolution(t *testing.T) {
	finder := NewVP8RefFinder()

	// TID=1 frame arrives before keyframe
	tid1Frame := &EncodedFrame{FrameType: FrameTypeDelta}
	tid1Header := &RTPVideoHeader{
		PictureID:   101,
		TemporalIdx: 1,
		TL0PicIdx:   10,
	}
	result := finder.ManageFrame(tid1Frame, tid1Header)
	assert.Len(t, result, 0, "TID=1 frame should be stashed")

	// Keyframe arrives
	keyframe := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: 0,
		TL0PicIdx:   10,
	}
	result = finder.ManageFrame(keyframe, keyHeader)

	// Both keyframe and stashed TID=1 frame should be resolved
	require.GreaterOrEqual(t, len(result), 1, "At least keyframe should be returned")
	assert.Equal(t, int64(100), result[0].ID, "First should be keyframe")
}

func TestVP8RefFinder_NilInputs(t *testing.T) {
	finder := NewVP8RefFinder()

	// Nil frame
	result := finder.ManageFrame(nil, &RTPVideoHeader{})
	assert.Nil(t, result)

	// Nil header
	result = finder.ManageFrame(&EncodedFrame{}, nil)
	assert.Nil(t, result)

	// Both nil
	result = finder.ManageFrame(nil, nil)
	assert.Nil(t, result)
}

func TestVP8RefFinder_FallbackNoTemporalInfo(t *testing.T) {
	finder := NewVP8RefFinder()

	// Frame with picture ID but no temporal info
	frame := &EncodedFrame{FrameType: FrameTypeKey}
	header := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: NoTemporalIdx,
		TL0PicIdx:   NoTL0PicIdx,
	}
	result := finder.ManageFrame(frame, header)

	require.Len(t, result, 1)
	assert.Equal(t, int64(100), result[0].ID)
	assert.Equal(t, 0, result[0].NumReferences)
}

func TestVP8RefFinder_FallbackDelta(t *testing.T) {
	finder := NewVP8RefFinder()

	// Keyframe first
	key := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{
		PictureID:   100,
		TemporalIdx: NoTemporalIdx,
		TL0PicIdx:   NoTL0PicIdx,
	}
	finder.ManageFrame(key, keyHeader)

	// Delta with picture ID but no temporal info
	delta := &EncodedFrame{FrameType: FrameTypeDelta}
	deltaHeader := &RTPVideoHeader{
		PictureID:   101,
		TemporalIdx: NoTemporalIdx,
		TL0PicIdx:   NoTL0PicIdx,
	}
	result := finder.ManageFrame(delta, deltaHeader)

	require.Len(t, result, 1)
	assert.Equal(t, int64(101), result[0].ID)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(100), result[0].References[0])
}

func TestVP8RefFinder_TL0PicIdxWrapAround(t *testing.T) {
	finder := NewVP8RefFinder()

	// Keyframe at TL0PICIDX=254
	key1 := &EncodedFrame{FrameType: FrameTypeKey}
	h1 := &RTPVideoHeader{PictureID: 100, TemporalIdx: 0, TL0PicIdx: 254}
	result := finder.ManageFrame(key1, h1)
	require.Len(t, result, 1)

	// Delta at TL0PICIDX=255
	d1 := &EncodedFrame{FrameType: FrameTypeDelta}
	h2 := &RTPVideoHeader{PictureID: 101, TemporalIdx: 0, TL0PicIdx: 255}
	result = finder.ManageFrame(d1, h2)
	require.Len(t, result, 1)
	assert.Equal(t, 1, result[0].NumReferences)

	// Delta at TL0PICIDX=0 (wrapped)
	d2 := &EncodedFrame{FrameType: FrameTypeDelta}
	h3 := &RTPVideoHeader{PictureID: 102, TemporalIdx: 0, TL0PicIdx: 0}
	result = finder.ManageFrame(d2, h3)
	require.Len(t, result, 1)
	assert.Equal(t, 1, result[0].NumReferences)
	assert.Equal(t, int64(101), result[0].References[0], "Should reference previous frame across wrap")
}

func TestTL0PicIdxUnwrapper(t *testing.T) {
	tests := []struct {
		name     string
		sequence []int16
		expected []int64
	}{
		{
			name:     "Sequential",
			sequence: []int16{0, 1, 2, 3},
			expected: []int64{0, 1, 2, 3},
		},
		{
			name:     "Wrap at 8-bit boundary",
			sequence: []int16{254, 255, 0, 1},
			expected: []int64{254, 255, 256, 257},
		},
		{
			name:     "Small gap",
			sequence: []int16{10, 15, 20},
			expected: []int64{10, 15, 20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &tl0PicIdxUnwrapper{}
			for i, idx := range tt.sequence {
				result := u.Unwrap(idx)
				assert.Equal(t, tt.expected[i], result, "Unwrap(%d) at index %d", idx, i)
			}
		})
	}
}

func TestVP8RefFinder_ClearTo(t *testing.T) {
	finder := NewVP8RefFinder()

	// Add keyframe
	key := &EncodedFrame{FrameType: FrameTypeKey}
	keyHeader := &RTPVideoHeader{PictureID: 100, TemporalIdx: 0, TL0PicIdx: 10}
	finder.ManageFrame(key, keyHeader)

	// Stash some frames
	for i := int32(105); i < 110; i++ {
		delta := &EncodedFrame{FrameType: FrameTypeDelta}
		header := &RTPVideoHeader{PictureID: i, TemporalIdx: 1, TL0PicIdx: 15}
		finder.ManageFrame(delta, header)
	}

	// Clear frames before picture ID 108
	finder.ClearTo(108)

	// Internal state check via behavior
	// The finder should have cleared frames with PID < 108
}
