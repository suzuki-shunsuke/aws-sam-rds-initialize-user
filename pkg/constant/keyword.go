package constant

import "errors"

const Result = "result"

var ErrNoBoolVariable = errors.New(`the variable "result" isn't defined`)
