// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package videoframe

import (
	"github.com/pion/logging"
)

// ReceiverInterceptorOption can be used to configure ReceiverInterceptor.
type ReceiverInterceptorOption func(r *ReceiverInterceptor) error

// WithPacketBufferSize sets the packet buffer size.
// Size must be a power of 2 (64, 128, 256, 512, 1024, 2048).
// Default is 512.
func WithPacketBufferSize(size uint16) ReceiverInterceptorOption {
	return func(r *ReceiverInterceptor) error {
		r.packetBufferSize = size
		return nil
	}
}

// WithLog sets a logger for the interceptor.
func WithLog(log logging.LeveledLogger) ReceiverInterceptorOption {
	return func(r *ReceiverInterceptor) error {
		r.log = log
		return nil
	}
}

// WithLoggerFactory sets a logger factory for the interceptor.
func WithLoggerFactory(loggerFactory logging.LoggerFactory) ReceiverInterceptorOption {
	return func(r *ReceiverInterceptor) error {
		r.loggerFactory = loggerFactory
		return nil
	}
}
