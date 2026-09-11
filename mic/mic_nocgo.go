//go:build !cgo

package mic

func open(*Mic, int) (func(), error) { return nil, ErrNoCapture }
