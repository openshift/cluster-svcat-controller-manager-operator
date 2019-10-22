package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog"
)

var (
	controllerManagerEnabled = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "service_catalog_controller_manager_enabled",
			Help: "Indicates whether Service Catalog controller manager is enabled",
		})
)

func init() {
	// do the MustRegister here
	prometheus.MustRegister(controllerManagerEnabled)
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
	controllerManagerEnabled.Inc()
}

// ControllerManagerDisabled - Indicates Service Catalog Controller Manager has
// been disabled
func ControllerManagerDisabled() {
	defer recoverMetricPanic()
	controllerManagerEnabled.Dec()
}
