package ot

// SetApplyHook lets external tests record which subtables applied.
func SetApplyHook(f func(gsub bool, kind, format int, extension bool)) { applyHook = f }
