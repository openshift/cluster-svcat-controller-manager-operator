package metrics

import (
	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog"
)

var (
	buildInfo = k8smetrics.NewGaugeVec(
		&k8smetrics.GaugeOpts{
			Name: "openshift_cluster_svcat_apiserver_operator_build_info",
			Help: "A metric with a constant '1' value labeled by major, minor, git commit & git version from which OpenShift Service Catalog Operator was built.",
		},
		[]string{"major", "minor", "gitCommit", "gitVersion"},
	)

	controllerManagerEnabled = k8smetrics.NewGauge(
		&k8smetrics.GaugeOpts{
			Name: "service_catalog_controller_manager_enabled",
			Help: "Indicates whether Service Catalog controller manager is enabled",
		})
)

func init() {
	// do the MustRegister here
	legacyregistry.MustRegister(buildInfo)
	legacyregistry.MustRegister(controllerManagerEnabled)
}

// We will never want to panic our operator because of metric saving.
// Therefore, we will recover our panics here and error log them
// for later diagnosis but will never fail the operator.
func recoverMetricPanic() {
	if r := recover(); r != nil {
		klog.Errorf("Recovering from metric function - %v", r)
	}
}

// ControllerManagerEnabled - Indicates Service Catalog Controller Manager has been enabled
func ControllerManagerEnabled() {
	defer recoverMetricPanic()
	controllerManagerEnabled.Set(1.0)
}

// ControllerManagerDisabled - Indicates Service Catalog Controller Manager has
// been disabled
func ControllerManagerDisabled() {
	defer recoverMetricPanic()
	controllerManagerEnabled.Set(0.0)
}

// RegisterVersion - Emits the operator's build information
func RegisterVersion(major, minor, gitCommit, gitVersion string) {
	defer recoverMetricPanic()
	buildInfo.WithLabelValues(major, minor, gitCommit, gitVersion).Set(1)
}
