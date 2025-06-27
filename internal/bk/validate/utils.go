package validate

import "errors"

func AggregateErr(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		var errStr string
		for _, e := range errs {
			errStr += e.Error() + "\n"
		}
		return errors.New(errStr)
	}
}
