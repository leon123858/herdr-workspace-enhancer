package model

import "errors"

var ErrConflict = errors.New("note changed in another editor; reload before saving")
