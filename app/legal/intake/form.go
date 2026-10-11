package intake

import "context"

// Form is an intake form. Only a published form accepts submissions.
type Form struct {
	ID        string
	Name      string
	Published bool
}

// FormStore reads intake forms.
type FormStore interface {
	// Form returns one form or ErrNotFound.
	Form(ctx context.Context, id string) (Form, error)
}
