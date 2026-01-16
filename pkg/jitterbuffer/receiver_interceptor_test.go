// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package jitterbuffer

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/internal/test"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
)

func TestBufferStart(t *testing.T) {
	buf := bytes.Buffer{}

	factory, err := NewInterceptor(
		WithLoggerFactory(logging.NewDefaultLoggerFactory()),
	)
	assert.NoError(t, err)

	testInterceptor, err := factory.NewInterceptor("")
	assert.NoError(t, err)

	assert.Zero(t, buf.Len())

	stream := test.NewMockStream(&interceptor.StreamInfo{
		SSRC:      123456,
		ClockRate: 90000,
	}, testInterceptor)
	defer func() {
		assert.NoError(t, stream.Close())
	}()

	stream.ReceiveRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{
		SenderSSRC: 123,
		MediaSSRC:  456,
	}})
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{
		SequenceNumber: uint16(0),
	}})

	// Give time for packets to be handled and stream written to.
	time.Sleep(50 * time.Millisecond)
	select {
	case pkt := <-stream.ReadRTP():
		assert.EqualValues(t, nil, pkt)
	default:
		// No data ready to read, this is what we expect
	}
	err = testInterceptor.Close()
	assert.NoError(t, err)
	assert.Zero(t, buf.Len())
}

func TestReceiverBuffersAndPlaysout(t *testing.T) {
	buf := bytes.Buffer{}

	factory, err := NewInterceptor(
		Log(logging.NewDefaultLoggerFactory().NewLogger("test")),
	)
	assert.NoError(t, err)

	testInterceptor, err := factory.NewInterceptor("")
	assert.NoError(t, err)

	assert.EqualValues(t, 0, buf.Len())

	stream := test.NewMockStream(&interceptor.StreamInfo{
		SSRC:      123456,
		ClockRate: 90000,
	}, testInterceptor)

	stream.ReceiveRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{
		SenderSSRC: 123,
		MediaSSRC:  456,
	}})
	for s := 0; s < 61; s++ {
		stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{
			SequenceNumber: uint16(s), //nolint:gosec // G115
		}})
	}
	// Give time for packets to be handled and stream written to.
	time.Sleep(50 * time.Millisecond)
	for s := 0; s < 10; s++ {
		read := <-stream.ReadRTP()
		seq := read.Packet.Header.SequenceNumber
		assert.EqualValues(t, uint16(s), seq) //nolint:gosec // G115
	}
	assert.NoError(t, stream.Close())
	err = testInterceptor.Close()
	assert.NoError(t, err)
}

func TestReceiverInterceptorTimeout(t *testing.T) {
	// This test verifies that missing packets are skipped after timeout.
	// The JitterBuffer uses a non-blocking design where ErrWaitingForPacket
	// is returned when a packet is missing, and the caller retries.
	// When the timeout elapses, the next Read() call will skip the missing packet.

	timeout := 50 * time.Millisecond

	factory, err := NewInterceptor(
		WithTimeout(timeout),
		WithMinPacketCount(3),
		Log(logging.NewDefaultLoggerFactory().NewLogger("test")),
	)
	assert.NoError(t, err)

	i, err := factory.NewInterceptor("")
	assert.NoError(t, err)
	ri := i.(*ReceiverInterceptor)

	// Directly test the JitterBuffer and timeout logic.
	// Push packets 0, 2, 3 (packet 1 is missing).
	ri.buffer.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 0}})
	ri.buffer.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 2}})
	ri.buffer.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 3}})

	// Force buffer into Emitting state.
	for j := 4; j < 54; j++ {
		ri.buffer.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(j)}}) //nolint:gosec
	}

	assert.Equal(t, Emitting, ri.buffer.state)

	// Pop packet 0 - should succeed.
	b := make([]byte, 1500)
	ri.m.Lock()
	n, _, err := ri.tryPop(b, nil)
	ri.m.Unlock()
	assert.NoError(t, err)
	assert.Greater(t, n, 0)

	pkt := &rtp.Packet{}
	assert.NoError(t, pkt.Unmarshal(b[:n]))
	assert.Equal(t, uint16(0), pkt.SequenceNumber)

	// Try to pop packet 1 - should return ErrWaitingForPacket.
	ri.m.Lock()
	_, _, err = ri.tryPop(b, nil)
	ri.m.Unlock()
	assert.ErrorIs(t, err, ErrWaitingForPacket)

	// Wait for timeout.
	time.Sleep(timeout + 10*time.Millisecond)

	// Now tryPop should skip packet 1 and return packet 2.
	ri.m.Lock()
	n, _, err = ri.tryPop(b, nil)
	ri.m.Unlock()
	assert.NoError(t, err)
	assert.Greater(t, n, 0)

	pkt = &rtp.Packet{}
	assert.NoError(t, pkt.Unmarshal(b[:n]))
	assert.Equal(t, uint16(2), pkt.SequenceNumber)

	assert.NoError(t, i.Close())
}

func TestReceiverInterceptorLatePacketArrival(t *testing.T) {
	timeout := 200 * time.Millisecond

	factory, err := NewInterceptor(
		WithTimeout(timeout),
		WithMinPacketCount(3),
		Log(logging.NewDefaultLoggerFactory().NewLogger("test")),
	)
	assert.NoError(t, err)

	testInterceptor, err := factory.NewInterceptor("")
	assert.NoError(t, err)

	stream := test.NewMockStream(&interceptor.StreamInfo{
		SSRC:      123456,
		ClockRate: 90000,
	}, testInterceptor)

	// Send packets 0, 2, 3 (packet 1 is missing initially).
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: 0}})
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: 2}})
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: 3}})

	time.Sleep(20 * time.Millisecond)

	// Packet 0 should be available.
	select {
	case pkt := <-stream.ReadRTP():
		assert.NotNil(t, pkt.Packet)
		assert.Equal(t, uint16(0), pkt.Packet.SequenceNumber)
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "expected packet 0")
	}

	// Now send the missing packet 1 before timeout.
	// This triggers a new Read() which pushes packet 1 and then pops it.
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: 1}})

	// Packet 1 should arrive (late packet was received).
	select {
	case pkt := <-stream.ReadRTP():
		assert.NotNil(t, pkt.Packet)
		assert.Equal(t, uint16(1), pkt.Packet.SequenceNumber)
	case <-time.After(200 * time.Millisecond):
		assert.Fail(t, "expected packet 1 (late arrival)")
	}

	// Send another packet to trigger Read for packet 2.
	stream.ReceiveRTP(&rtp.Packet{Header: rtp.Header{SequenceNumber: 4}})

	// Packet 2 should be available.
	select {
	case pkt := <-stream.ReadRTP():
		assert.NotNil(t, pkt.Packet)
		assert.Equal(t, uint16(2), pkt.Packet.SequenceNumber)
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "expected packet 2")
	}

	assert.NoError(t, stream.Close())
	assert.NoError(t, testInterceptor.Close())
}
