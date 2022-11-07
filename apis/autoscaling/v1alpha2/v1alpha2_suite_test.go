package v1alpha2_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestV1alpha2(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V1alpha2 Suite")
}
