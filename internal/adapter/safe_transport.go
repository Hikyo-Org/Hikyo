package adapter

// SafeTransportError hides receiver-controlled URLs and HTTP parser messages
// from error formatting and production logs. The original chain remains
// available only to typed error predicates, including cancellation and timeout
// classification; no cause text is interpolated into the safe message.
func SafeTransportError(cause error) error {
	if cause == nil {
		return nil
	}
	return safeTransportError{cause: cause}
}

type safeTransportError struct{ cause error }

func (safeTransportError) Error() string   { return "provider transport request failed" }
func (e safeTransportError) Unwrap() error { return e.cause }
