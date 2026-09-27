package cli

import "errors"

// errorsAs is errors.As with a name that reads well at call sites.
func errorsAs(err error, target any) bool { return errors.As(err, target) }
