// File: grpolicy_test.go

package grpolicy_test

import (
	"testing"

	"github.com/gourdian25/grpolicy"
	"github.com/gourdian25/grpolicy/conformance"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func() (grpolicy.Engine, error) {
		return grpolicy.NewEngine()
	})
}
