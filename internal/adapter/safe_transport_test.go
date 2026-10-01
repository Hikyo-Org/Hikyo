package adapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

type transportTimeoutCanary struct{}

func (transportTimeoutCanary) Error() string   { return "private-receiver-secret" }
func (transportTimeoutCanary) Timeout() bool   { return true }
func (transportTimeoutCanary) Temporary() bool { return true }

func TestSafeTransportErrorRetainsPredicatesWithoutCauseText(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, transportTimeoutCanary{}} {
		wrapped := &url.Error{Op: "Post", URL: "https://receiver/private-receiver-secret", Err: cause}
		safe := fmt.Errorf("provider request: %w", SafeTransportError(wrapped))
		if !errors.Is(safe, cause) {
			t.Fatalf("cause predicate was lost for %T", cause)
		}
		var timeout net.Error
		if !errors.As(safe, &timeout) || timeout.Timeout() != wrapped.Timeout() {
			t.Fatalf("net.Error timeout classification lost for %T", cause)
		}
		for _, text := range []string{safe.Error(), fmt.Sprintf("%v", safe), fmt.Sprintf("%+v", safe)} {
			if strings.Contains(text, "private-receiver-secret") {
				t.Fatalf("cause text exposed: %s", text)
			}
		}
	}
	if SafeTransportError(nil) != nil {
		t.Fatal("nil transport result became an error")
	}
}
