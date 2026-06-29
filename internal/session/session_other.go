//go:build !windows

package session

import (
	"context"
	"fmt"
)

func Run(_ context.Context, _ Config) (int, error) {
	return 1, fmt.Errorf("workofpoetry requires Windows 10 1809+ or Windows Server 2019+ with ConPTY support")
}
