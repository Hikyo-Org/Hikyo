package server

// deliveryRefusal is constructed only after the matched delivery operation
// returns its authenticated tenant refusal. It preserves the uniform body and
// carries no scope, identity, credential, or existence information.
type deliveryRefusal struct{ cause error }

func (e *deliveryRefusal) Error() string { return e.cause.Error() }
func (e *deliveryRefusal) Unwrap() error { return e.cause }
