package operator

import (
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	operatorinformers "github.com/openshift/client-go/operator/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/operator/v311_00_assets"
	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/util"
)

func RunOperator(ctx *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(ctx.ProtoKubeConfig)
	if err != nil {
		return err
	}
	operatorclient, err := operatorclient.NewForConfig(ctx.KubeConfig)
	if err != nil {
		return err
	}

	configClient, err := configv1client.NewForConfig(ctx.KubeConfig)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(ctx.KubeConfig)
	if err != nil {
		return err
	}

	v1helpers.EnsureOperatorConfigExists(
		dynamicClient,
		v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/operator-config.yaml"),
		schema.GroupVersionResource{Group: operatorv1.GroupName, Version: operatorv1.GroupVersion.Version, Resource: "servicecatalogcontrollermanagers"},
	)

	operatorConfigInformers := operatorinformers.NewSharedInformerFactory(operatorclient, 10*time.Minute)
	kubeInformersForServiceCatalogControllerManagerNamespace := informers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute, informers.WithNamespace(targetNamespaceName))
	kubeInformersForOperatorNamespace := informers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute, informers.WithNamespace(util.OperatorNamespaceName))
	configInformers := configinformers.NewSharedInformerFactory(configClient, 10*time.Minute)

	operator := NewServiceCatalogControllerManagerOperator(
		os.Getenv("IMAGE"),
		operatorConfigInformers.Operator().V1().ServiceCatalogControllerManagers(),
		kubeInformersForServiceCatalogControllerManagerNamespace,
		operatorclient.OperatorV1(),
		kubeClient,
		ctx.EventRecorder,
	)

	opClient := &operatorClient{
		informers: operatorConfigInformers,
		client:    operatorclient.OperatorV1(),
	}

	clusterOperatorStatus := status.NewClusterOperatorStatusController(
		"service-catalog-controller-manager",
		[]configv1.ObjectReference{},
		configClient.ConfigV1(),
		opClient,
		status.NewVersionGetter(),
		ctx.EventRecorder,
	)

	operatorConfigInformers.Start(ctx.Done())
	kubeInformersForServiceCatalogControllerManagerNamespace.Start(ctx.Done())
	kubeInformersForOperatorNamespace.Start(ctx.Done())
	configInformers.Start(ctx.Done())

	go operator.Run(1, ctx.Done())
	go clusterOperatorStatus.Run(1, ctx.Done())

	<-ctx.Done()
	return fmt.Errorf("stopped")
}
