package operator

import (
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configfake "github.com/openshift/client-go/config/clientset/versioned/fake"
	operatorfake "github.com/openshift/client-go/operator/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorv1helpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestUpgradeable(t *testing.T) {
	testCases := []struct {
		name            string
		managementState operatorapiv1.ManagementState
		expectedStatus  operatorapiv1.ConditionStatus
		expectedMessage string
		substring       bool
	}{
		{
			name:            "in managed state, upgradeable should be false",
			managementState: operatorapiv1.Managed,
			expectedStatus:  operatorapiv1.ConditionFalse,
			substring:       true,
			expectedMessage: "https://docs.openshift.com/container-platform/4.4/applications/service_brokers/installing-service-catalog.html",
		},
		{
			name:            "in unmanaged state, upgradeable should be true",
			managementState: operatorapiv1.Unmanaged,
			expectedStatus:  operatorapiv1.ConditionTrue,
			substring:       true,
			expectedMessage: "unmanaged state, upgrades are possible",
		},
		{
			name:            "in removed state, upgradeable should be true",
			managementState: operatorapiv1.Removed,
			expectedStatus:  operatorapiv1.ConditionTrue,
			substring:       true,
			expectedMessage: "removed state, upgrades are possible",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			// create kubeclient
			kubeClient := fake.NewSimpleClientset(
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "controller-manager",
						Namespace:  "openshift-service-catalog-controller-manager",
						Generation: 100,
					},
					Spec: appsv1.DaemonSetSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "fakeContainer",
										Env: []corev1.EnvVar{
											{
												Name:  "HTTP_PROXY",
												Value: "http://0.0.0.0:8080",
											},
										},
									},
								},
							},
						},
					},
				})

			// create operator config
			operatorConfig := &operatorv1.ServiceCatalogControllerManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster",
					Generation: 100,
				},
				Spec: operatorv1.ServiceCatalogControllerManagerSpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: tc.managementState,
					},
				},
				Status: operatorv1.ServiceCatalogControllerManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						ObservedGeneration: 100,
					},
				},
			}

			// create controller manager client and proxy config client
			controllerManagerOperatorClient := operatorfake.NewSimpleClientset(operatorConfig)
			proxyConfigClient := configfake.NewSimpleClientset(&configv1.Proxy{})

			// create dynamic client
			dynamicScheme := runtime.NewScheme()
			dynamicScheme.AddKnownTypeWithName(schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "ServiceMonitor"},
				&unstructured.Unstructured{})
			dynamicClient := dynamicfake.NewSimpleDynamicClient(dynamicScheme)

			// Finally create the operator we need to test
			svcat := ServiceCatalogControllerManagerOperator{
				kubeClient:           kubeClient,
				recorder:             events.NewInMemoryRecorder(""),
				configClient:         proxyConfigClient,
				operatorConfigClient: controllerManagerOperatorClient.OperatorV1(),
				dynamicClient:        dynamicClient,
			}

			// Test the sync method
			err := svcat.sync()
			if err != nil {
				t.Fatal("Unexpected error from sync: " + err.Error())
			}

			// verify results
			result, err := controllerManagerOperatorClient.OperatorV1().
				ServiceCatalogControllerManagers().Get("cluster", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}

			condition := operatorv1helpers.FindOperatorCondition(
				result.Status.Conditions, operatorapiv1.OperatorStatusTypeUpgradeable)
			if condition == nil {
				t.Fatal("nil condition")
			}

			// verify the status
			if condition.Status != tc.expectedStatus {
				t.Fatalf("expected %v but received %v", tc.expectedStatus, condition.Status)
			}

			// verify the state messages
			if tc.substring {
				if !strings.Contains(condition.Message, tc.expectedMessage) {
					t.Fatalf("expected to find %v in the message: %v", tc.expectedMessage, condition.Message)
				}
			} else {
				if condition.Message != tc.expectedMessage {
					t.Fatalf("expected %v to match message: %v", tc.expectedMessage, condition.Message)
				}
			}

		})
	}
}
