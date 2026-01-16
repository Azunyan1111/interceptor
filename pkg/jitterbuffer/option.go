// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package jitterbuffer

import (
	"time"

	"github.com/pion/logging"
)

// ReceiverInterceptorOption can be used to configure ReceiverInterceptor.
type ReceiverInterceptorOption func(d *ReceiverInterceptor) error

// Log sets a logger for the interceptor.
func Log(log logging.LeveledLogger) ReceiverInterceptorOption {
	return func(d *ReceiverInterceptor) error {
		d.log = log

		return nil
	}
}

// WithLoggerFactory sets a logger factory for the interceptor.
func WithLoggerFactory(loggerFactory logging.LoggerFactory) ReceiverInterceptorOption {
	return func(d *ReceiverInterceptor) error {
		d.loggerFactory = loggerFactory

		return nil
	}
}

// WithTimeout sets the timeout duration for waiting missing packets.
// When a packet is missing, the interceptor will wait up to this duration
// for the packet to arrive before skipping it.
// Default: 100ms.
func WithTimeout(timeout time.Duration) ReceiverInterceptorOption {
	return func(d *ReceiverInterceptor) error {
		d.timeout = timeout

		return nil
	}
}

// WithMinPacketCount sets the minimum packet count before playout begins.
// Default: 50.
func WithMinPacketCount(count uint16) ReceiverInterceptorOption {
	return func(d *ReceiverInterceptor) error {
		d.minPacketCount = count

		return nil
	}
}
