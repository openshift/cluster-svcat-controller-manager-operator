package operator

import (
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/operator"
	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

func NewOperator() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("svcat-controller-manager-operator", version.Get(), operator.RunOperator).
		NewCommand()
	cmd.Use = "operator"
	cmd.Short = "Start the Cluster svcat-controller-manager Operator"

	return cmd
}
