package main

import (
	operatorapiv1 "github.com/openshift/api/operator/v1"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
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
	log.Info("Starting openshift-service-catalog-controller-manager-remover job")

	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		clientConfig, err = createClientConfigFromFile(homedir.HomeDir() + "/.kube/config")
		if err != nil {
			//log.Error("Failed to create LocalClientSet")
			//return nil, err
			log.Error("Failed to create LocalClientSet")
			panic(err.Error())
		}
	}

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		panic(err.Error())
	}

	operatorClient, err := operatorclient.NewForConfig(clientConfig)
	if err != nil {
		log.Errorf("problem getting operator client, error %v", err)
	}
	operatorConfigClient := operatorClient.OperatorV1()
	operatorConfig, err := operatorConfigClient.ServiceCatalogControllerManagers().Get("cluster", metav1.GetOptions{})
	if err != nil {
		log.Errorf("problem getting ServiceCatalogControllerManage CR, error %v", err)
	}

	switch operatorConfig.Spec.ManagementState {
	case operatorapiv1.Managed:
		log.Warning("We found a cluster-svcat-controller-manager-operator in Managed state. Aborting")
		break
	case operatorapiv1.Unmanaged:
		log.Info("ServiceCatalogControllerManager managementState is 'Unmanaged'")
		log.Infof("Removing target namespace %s", targetNamespaceName)
		if err := kubeClient.CoreV1().Namespaces().Delete(targetNamespaceName, nil); err != nil && !apierrors.IsNotFound(err) {
			log.Errorf("problem removing target namespace [%s] :  %v", targetNamespaceName, err)
		}
		log.Info("Removing the ServiceCatalogControllerManager CR")
		err = operatorConfigClient.ServiceCatalogControllerManagers().Delete("cluster", &metav1.DeleteOptions{})
		if err != nil {
			log.Errorf("ServiceCatalogControllerManager cr deletion failed: %v", err)
		} else {
			log.Info("ServiceCatalogControllerManager cr removed successfully.")
		}
		break
	case operatorapiv1.Removed:
		log.Info("ServiceCatalogControllerManager managementState is 'Removed'")
		log.Infof("Removing target namespace %s", targetNamespaceName)
		// TODO: check to see if there are any remanents of the Service Catalog
		if err := kubeClient.CoreV1().Namespaces().Delete(targetNamespaceName, nil); err != nil && !apierrors.IsNotFound(err) {
			log.Errorf("problem removing target namespace [%s] :  %v", targetNamespaceName, err)
		}
		log.Info("Removing the ServiceCatalogControllerManager CR")
		err = operatorConfigClient.ServiceCatalogControllerManagers().Delete("cluster", &metav1.DeleteOptions{})
		if err != nil {
			log.Errorf("ServiceCatalogControllerManager cr deletion failed: %v", err)
		} else {
			log.Info("ServiceCatalogControllerManager cr removed successfully.")
		}
		break
	default:
		log.Error("Unknown managementState")
	}
}
