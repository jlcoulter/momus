package main

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestIsServerUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "wrapped connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
			want: true,
		},
		{
			name: "dns failure",
			err:  &net.DNSError{Err: "no such host", Name: "localhost", IsNotFound: true},
			want: true,
		},
		{
			name: "timeout",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: timeoutError{}},
			want: true,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServerUnavailable(tc.err); got != tc.want {
				t.Fatalf("isServerUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// timeoutError implements net.Error and reports itself as a timeout.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
