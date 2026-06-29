//go:build !windows

package term

import "os"

type RawState struct{}

func MakeRaw(_ *os.File) (*RawState, error) { return &RawState{}, nil }
func (s *RawState) Restore() error          { return nil }
func Size(_ *os.File) (int, int)            { return 80, 25 }
func EnableVT(_ *os.File) error             { return nil }
