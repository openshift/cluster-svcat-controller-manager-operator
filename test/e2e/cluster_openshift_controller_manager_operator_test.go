package e2e

import (
	"testing"

	"github.com/openshift/cluster-svcat-controller-manager-operator/test/framework"
)

func TestClusterOpenshiftControllerManagerOperator(t *testing.T) {
	client := framework.MustNewClientset(t, nil)
	// make sure the operator is fully up
	framework.MustEnsureClusterOperatorStatusIsSet(t, client)
}
