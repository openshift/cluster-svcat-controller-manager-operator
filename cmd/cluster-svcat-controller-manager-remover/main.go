package main

import (
	"fmt"

	operatorapiv1 "github.com/openshift/api/operator/v1"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog"
)

var targetNamespaceName = "openshift-service-catalog-controller-manager-operator"

func createClientConfigFromFile(configPath string) (*rest.Config, error) {
	clientConfig, err := clientcmd.LoadFromFile(configPath)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.NewDefaultClientConfig(*clientConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, err
	}
	return config, nil
}

func main() {
	fmt.Println("job")
	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		clientConfig, err = createClientConfigFromFile(homedir.HomeDir() + "/.kube/config")
		if err != nil {
			//log.Error("Failed to create LocalClientSet")
			//return nil, err
			klog.Error("Failed to create LocalClientSet")
			panic(err.Error())
		}
	}

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		panic(err.Error())
	}

	operatorClient, err := operatorclient.NewForConfig(clientConfig)
	if err != nil {
		fmt.Println("error %v", err)
	}
	operatorConfigClient := operatorClient.OperatorV1()
	operatorConfig, err := operatorConfigClient.ServiceCatalogControllerManagers().Get("cluster", metav1.GetOptions{})
	if err != nil {
		fmt.Println("error %v", err)
	}

	switch operatorConfig.Spec.ManagementState {
	case operatorapiv1.Managed:
		fmt.Println("We found a cluster-svcat-controller-manager-operator in Managed state. Aborting")
		break
	case operatorapiv1.Unmanaged:
		if err := kubeClient.CoreV1().Namespaces().Delete(targetNamespaceName, nil); err != nil && !apierrors.IsNotFound(err) {
			fmt.Println("error %v", err)
		}
		fmt.Println("removing the CR")
		err = operatorConfigClient.ServiceCatalogControllerManagers().Delete("cluster", &metav1.DeleteOptions{})
		if err != nil {
			fmt.Println("cr deletion failed: %v", err)
		} else {
			fmt.Println("looks like svcat cm cr removed")
		}
		break
	case operatorapiv1.Removed:
		fmt.Println("status is in removed")
		// TODO: check to see if there are any remanents of the Service Catalog
		if err := kubeClient.CoreV1().Namespaces().Delete(targetNamespaceName, nil); err != nil && !apierrors.IsNotFound(err) {
			fmt.Println("error %v", err)
		}
		fmt.Println("removing the CR")
		err = operatorConfigClient.ServiceCatalogControllerManagers().Delete("cluster", &metav1.DeleteOptions{})
		if err != nil {
			fmt.Println("cr deletion failed: %v", err)
		} else {
			fmt.Println("looks like svcat cm cr removed")
		}
		break
	default:
		fmt.Println("Unknown management state")
	}
}
