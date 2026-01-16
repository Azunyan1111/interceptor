// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package jitterbuffer

import (
	"errors"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

// ErrWaitingForPacket is returned when a packet is missing and the interceptor
// is waiting for it to arrive. The caller should retry the read operation.
var ErrWaitingForPacket = errors.New("waiting for missing packet")

// InterceptorFactory is a interceptor.Factory for a GeneratorInterceptor.
type InterceptorFactory struct {
	opts []ReceiverInterceptorOption
}

// NewInterceptor constructs a new ReceiverInterceptor.
func (g *InterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	receiverInterceptor := &ReceiverInterceptor{
		buffer:         nil, // initialized after options are applied
		timeout:        100 * time.Millisecond,
		minPacketCount: 50,
		gapDetectedAt:  make(map[uint16]time.Time),
	}

	for _, opt := range g.opts {
		if err := opt(receiverInterceptor); err != nil {
			return nil, err
		}
	}

	// Initialize JitterBuffer with the configured minPacketCount.
	receiverInterceptor.buffer = New(WithMinimumPacketCount(receiverInterceptor.minPacketCount))

	if receiverInterceptor.loggerFactory == nil {
		receiverInterceptor.loggerFactory = logging.NewDefaultLoggerFactory()
	}
	if receiverInterceptor.log == nil {
		receiverInterceptor.log = receiverInterceptor.loggerFactory.NewLogger("jitterbuffer")
	}

	return receiverInterceptor, nil
}

// ReceiverInterceptor places a JitterBuffer in the chain to smooth packet arrival
// and allow for network jitter
//
//	The Interceptor is designed to fit in a RemoteStream
//	pipeline and buffer incoming packets for a short period (currently
//	defaulting to 50 packets) before emitting packets to be consumed by the
//	next step in the pipeline.
//
//	The caller must ensure they are prepared to handle an
//	ErrPopWhileBuffering in the case that insufficient packets have been
//	received by the jitter buffer. The caller should retry the operation
//	at some point later as the buffer may have been filled in the interim.
//
//	The caller should also be aware that an ErrBufferUnderrun may be
//	returned in the case that the initial buffering was sufficient and
//	playback began but the caller is consuming packets (or they are not
//	arriving) quickly enough.
type ReceiverInterceptor struct {
	interceptor.NoOp
	buffer        *JitterBuffer
	m             sync.Mutex
	wg            sync.WaitGroup
	log           logging.LeveledLogger
	loggerFactory logging.LoggerFactory

	// timeout is the duration to wait for missing packets before skipping.
	timeout time.Duration
	// minPacketCount is the minimum packet count before playout begins.
	minPacketCount uint16
	// gapDetectedAt tracks when each missing packet was first detected.
	gapDetectedAt map[uint16]time.Time
}

// NewInterceptor returns a new InterceptorFactory.
func NewInterceptor(opts ...ReceiverInterceptorOption) (*InterceptorFactory, error) {
	return &InterceptorFactory{opts}, nil
}

// BindRemoteStream lets you modify any incoming RTP packets. It is called once for per RemoteStream.
// The returned method will be called once per rtp packet.
func (i *ReceiverInterceptor) BindRemoteStream(
	_ *interceptor.StreamInfo, reader interceptor.RTPReader,
) interceptor.RTPReader {
	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		buf := make([]byte, len(b))
		n, attr, err := reader.Read(buf, a)
		if err != nil {
			return n, attr, err
		}
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf); err != nil {
			return 0, nil, err
		}

		i.m.Lock()
		defer i.m.Unlock()

		i.buffer.Push(packet)

		if i.buffer.state != Emitting {
			return n, attr, ErrPopWhileBuffering
		}

		// Try to pop with timeout-based skip handling.
		return i.tryPop(b, attr)
	})
}

// tryPop attempts to pop a packet from the buffer.
// If a packet is missing, it checks if the timeout has elapsed and skips if necessary.
// Returns ErrWaitingForPacket if still waiting for a missing packet.
func (i *ReceiverInterceptor) tryPop(b []byte, attr interceptor.Attributes) (int, interceptor.Attributes, error) {
	for {
		playoutHead := i.buffer.PlayoutHead()
		newPkt, err := i.buffer.Pop()

		if err == nil {
			// Success: clear the detection time and return the packet.
			delete(i.gapDetectedAt, playoutHead)
			nlen, marshalErr := newPkt.MarshalTo(b)

			return nlen, attr, marshalErr
		}

		// Check if it's a missing packet error.
		if !errors.Is(err, ErrNotFound) {
			return 0, nil, err
		}

		// Record the first detection time for this missing packet.
		now := time.Now()
		if _, exists := i.gapDetectedAt[playoutHead]; !exists {
			i.gapDetectedAt[playoutHead] = now
		}

		detectedAt := i.gapDetectedAt[playoutHead]
		elapsed := now.Sub(detectedAt)

		if elapsed < i.timeout {
			// Still waiting for the packet.
			return 0, attr, ErrWaitingForPacket
		}

		// Timeout: skip this packet and try the next one.
		i.log.Debugf("packet %d timed out after %v, skipping", playoutHead, elapsed)
		delete(i.gapDetectedAt, playoutHead)
		i.buffer.SetPlayoutHead(playoutHead + 1)
		// Continue to try the next packet.
	}
}

// UnbindRemoteStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (i *ReceiverInterceptor) UnbindRemoteStream(_ *interceptor.StreamInfo) {
	defer i.wg.Wait()
	i.m.Lock()
	defer i.m.Unlock()
	i.buffer.Clear(true)
}

// Close closes the interceptor.
func (i *ReceiverInterceptor) Close() error {
	defer i.wg.Wait()
	i.m.Lock()
	defer i.m.Unlock()
	i.buffer.Clear(true)

	return nil
}
