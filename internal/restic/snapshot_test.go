package restic_test

import (
	"testing"
	"time"

	"github.com/quinn/restic/internal/restic"
	rtest "github.com/quinn/restic/internal/test"
)

func TestNewSnapshot(t *testing.T) {
	paths := []string{"/home/foobar"}

	_, err := restic.NewSnapshot(paths, nil, "foo", time.Now())
	rtest.OK(t, err)
}
