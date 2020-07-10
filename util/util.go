package util

import (
	"fmt"
	"net/http"
	"time"
)

// RedirectError specific error type that happens on redirection
type RedirectError struct {
	msg string
}

// Error message
func (s *RedirectError) Error() string {
	return s.msg
}

// NewRedirectError return a redirect error message
func NewRedirectError(m string) *RedirectError {
	return &RedirectError{msg: m}
}

// ByteSize a helper struct that implements the String() method and returns a human readable result. Very useful for %v formatting.
type ByteSize struct {
	Size float64
}

func (s ByteSize) String() string {
	var rt float64
	var suffix string
	const (
		Byte  = 1
		KByte = Byte * 1024
		MByte = KByte * 1024
		GByte = MByte * 1024
	)

	if s.Size > GByte {
		rt = s.Size / GByte
		suffix = "GB"
	} else if s.Size > MByte {
		rt = s.Size / MByte
		suffix = "MB"
	} else if s.Size > KByte {
		rt = s.Size / KByte
		suffix = "KB"
	} else {
		rt = s.Size
		suffix = "bytes"
	}

	return fmt.Sprintf("%.2f%v", rt, suffix)
}

// MaxDuration compares d1 and d2 and return the highest value
func MaxDuration(d1 time.Duration, d2 time.Duration) time.Duration {
	if d1 > d2 {
		return d1
	}
	return d2
}

// MinDuration compares d1 and d2 and return the lowest value
func MinDuration(d1 time.Duration, d2 time.Duration) time.Duration {
	if d1 < d2 {
		return d1
	}
	return d2
}

// EstimateHTTPHeadersSize had to create this because headers size was not counted
func EstimateHTTPHeadersSize(headers http.Header) int64 {
	var result int64 = 0

	for k, v := range headers {
		result += int64(len(k) + len(": \r\n"))
		for _, s := range v {
			result += int64(len(s))
		}
	}

	return result + int64(len("\r\n"))
}
