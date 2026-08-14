package notify

import "context"

// Sender dispatches a Message through one specific provider's transport and
// payload shape. Every providers/* package (including providers/email)
// returns a type implementing this interface, so a host application can
// treat all configured notification destinations uniformly.
type Sender interface {
	// Send delivers msg through the provider. Implementations should
	// respect ctx cancellation/deadlines and return a wrapped error on
	// failure rather than panicking or swallowing errors.
	Send(ctx context.Context, msg Message) error
}
