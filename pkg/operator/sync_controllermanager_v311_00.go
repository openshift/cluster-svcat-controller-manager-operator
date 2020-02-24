package operator

import (
	"fmt"
	"os"
	"strings"

	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/operator/v311_00_assets"
	"github.com/openshift/cluster-svcat-controller-manager-operator/pkg/util"

	configv1 "github.com/openshift/api/config/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehash"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
)

const (
	httpProxyEnvVar  = "HTTP_PROXY"
	httpsProxyEnvVar = "HTTPS_PROXY"
	noProxyEnvVar    = "NO_PROXY"
	trustedCABundle  = "trusted-ca-bundle"
)

// syncServiceCatalogControllerManager_v311_00_to_latest takes care of synchronizing (not upgrading) the thing we're managing.
// most of the time the sync method will be good for a large span of minor versions
func syncServiceCatalogControllerManager_v311_00_to_latest(c ServiceCatalogControllerManagerOperator, originalOperatorConfig *operatorapiv1.ServiceCatalogControllerManager, proxyConfig *configv1.Proxy) (bool, error) {
	errors := []error{}
	var err error
	operatorConfig := originalOperatorConfig.DeepCopy()

	// react to API change
	clientHolder := resourceapply.NewKubeClientHolder(c.kubeClient)
	directResourceResults := resourceapply.ApplyDirectly(clientHolder, c.recorder, v311_00_assets.Asset,
		"v3.11.0/openshift-svcat-controller-manager/ns.yaml",
		"v3.11.0/openshift-svcat-controller-manager/crb-catalog-controller.yaml",
		"v3.11.0/openshift-svcat-controller-manager/crb-controller-namespace-viewer-binding.yaml",
		"v3.11.0/openshift-svcat-controller-manager/cr-catalog-controller.yaml",
		"v3.11.0/openshift-svcat-controller-manager/rolebinding-cluster-info-configmap.yaml",
		"v3.11.0/openshift-svcat-controller-manager/rolebinding-configmap-accessor.yaml",
		"v3.11.0/openshift-svcat-controller-manager/role-cluster-info-configmap.yaml",
		"v3.11.0/openshift-svcat-controller-manager/role-configmap-accessor.yaml",
		"v3.11.0/openshift-svcat-controller-manager/sa.yaml",
		"v3.11.0/openshift-svcat-controller-manager/svc.yaml",
		"v3.11.0/openshift-svcat-controller-manager/servicemonitor-role.yaml",
		"v3.11.0/openshift-svcat-controller-manager/servicemonitor-rolebinding.yaml",
	)
	resourcesThatForceRedeployment := sets.NewString("v3.11.0/openshift-svcat-controller-manager/sa.yaml")
	forceRollout := false

	for _, currResult := range directResourceResults {
		if currResult.Error != nil {
			errors = append(errors, fmt.Errorf("%q (%T): %v", currResult.File, currResult.Type, currResult.Error))
			continue
		}

		if currResult.Changed && resourcesThatForceRedeployment.Has(currResult.File) {
			forceRollout = true
		}
	}

	_, err = resourceapply.ApplyServiceMonitor(c.dynamicClient, c.recorder, v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/servicemonitor.yaml"))
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "servicemonitor", err))
	}

	_, configMapModified, err := manageServiceCatalogControllerManagerConfigMap_v311_00_to_latest(c.kubeClient, c.kubeClient.CoreV1(), c.recorder, operatorConfig)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "configmap", err))
	}

	// Handle the Trusted CA configmap
	_, trustedCAModified, err := manageServiceCatalogControllerManagerTrustedCAConfigMap_v311_00_to_latest(c.kubeClient, c.kubeClient.CoreV1(), c.recorder, operatorConfig)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "configmap", err))
	}

	// the kube-apiserver is the source of truth for client CA bundles
	clientCAModified, err := manageServiceCatalogControllerManagerClientCA_v311_00_to_latest(c.kubeClient.CoreV1(), c.recorder)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "client-ca", err))
	}

	forceRollout = forceRollout || operatorConfig.ObjectMeta.Generation != operatorConfig.Status.ObservedGeneration
	forceRollout = forceRollout || configMapModified || clientCAModified || trustedCAModified

	// our configmaps and secrets are in order, now it is time to create the DS
	// TODO check basic preconditions here
	actualDaemonSet, _, err := manageServiceCatalogControllerManagerDeployment_v311_00_to_latest(c.kubeClient.AppsV1(), c.recorder, operatorConfig, c.targetImagePullSpec, operatorConfig.Status.Generations, forceRollout, proxyConfig, c.kubeClient.CoreV1())
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "deployment", err))
	}

	// manage status
	if actualDaemonSet.Status.NumberAvailable > 0 {
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:   operatorapiv1.OperatorStatusTypeAvailable,
			Status: operatorapiv1.ConditionTrue,
		})
	} else {
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:    operatorapiv1.OperatorStatusTypeAvailable,
			Status:  operatorapiv1.ConditionFalse,
			Reason:  "NoPodsAvailable",
			Message: "no daemon pods available on any node.",
		})
	}
	if actualDaemonSet.Status.NumberAvailable > 0 && actualDaemonSet.Status.UpdatedNumberScheduled == actualDaemonSet.Status.CurrentNumberScheduled {
		if len(actualDaemonSet.Annotations[util.VersionAnnotation]) > 0 {
			operatorConfig.Status.Version = actualDaemonSet.Annotations[util.VersionAnnotation]
		}
	}
	var progressingMessages []string
	if actualDaemonSet != nil && actualDaemonSet.ObjectMeta.Generation != actualDaemonSet.Status.ObservedGeneration {
		progressingMessages = append(progressingMessages, fmt.Sprintf("daemonset/controller-manager: observed generation is %d, desired generation is %d.", actualDaemonSet.Status.ObservedGeneration, actualDaemonSet.ObjectMeta.Generation))
	}
	if operatorConfig.ObjectMeta.Generation != operatorConfig.Status.ObservedGeneration {
		progressingMessages = append(progressingMessages, fmt.Sprintf("servicecatalogcontrollermanagers.operator.openshift.io/cluster: observed generation is %d, desired generation is %d.", operatorConfig.Status.ObservedGeneration, operatorConfig.ObjectMeta.Generation))
	}
	if len(progressingMessages) == 0 {
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:   operatorapiv1.OperatorStatusTypeProgressing,
			Status: operatorapiv1.ConditionFalse,
			Reason: "DesiredStateAchieved",
		})
	} else {
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:    operatorapiv1.OperatorStatusTypeProgressing,
			Status:  operatorapiv1.ConditionTrue,
			Reason:  "DesiredStateNotYetAchieved",
			Message: strings.Join(progressingMessages, "\n"),
		})
	}

	operatorConfig.Status.ObservedGeneration = operatorConfig.ObjectMeta.Generation
	resourcemerge.SetDaemonSetGeneration(&operatorConfig.Status.Generations, actualDaemonSet)

	if len(errors) > 0 {
		message := ""
		for _, err := range errors {
			message = message + err.Error() + "\n"
		}
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:    workloadDegradedCondition,
			Status:  operatorapiv1.ConditionTrue,
			Message: message,
			Reason:  "SyncError",
		})
	} else {
		v1helpers.SetOperatorCondition(&operatorConfig.Status.Conditions, operatorapiv1.OperatorCondition{
			Type:   workloadDegradedCondition,
			Status: operatorapiv1.ConditionFalse,
		})
	}

	if !equality.Semantic.DeepEqual(operatorConfig.Status, originalOperatorConfig.Status) {
		if _, err := c.operatorConfigClient.ServiceCatalogControllerManagers().UpdateStatus(operatorConfig); err != nil {
			return false, err
		}
	}

	if len(errors) > 0 {
		return true, nil
	}
	return false, nil
}

func manageServiceCatalogControllerManagerClientCA_v311_00_to_latest(client coreclientv1.CoreV1Interface, recorder events.Recorder) (bool, error) {
	const apiserverClientCA = "client-ca"
	_, caChanged, err := resourceapply.SyncConfigMap(client, recorder, kubeAPIServerNamespaceName, apiserverClientCA, targetNamespaceName, apiserverClientCA, []metav1.OwnerReference{})
	if err != nil {
		return false, err
	}
	return caChanged, nil
}

func manageServiceCatalogControllerManagerConfigMap_v311_00_to_latest(kubeClient kubernetes.Interface, client coreclientv1.ConfigMapsGetter, recorder events.Recorder, operatorConfig *operatorapiv1.ServiceCatalogControllerManager) (*corev1.ConfigMap, bool, error) {

	configMap := resourceread.ReadConfigMapV1OrDie(v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/cm.yaml"))
	defaultConfig := v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/defaultconfig.yaml")
	requiredConfigMap, _, err := resourcemerge.MergeConfigMap(configMap, "config.yaml", nil, defaultConfig, operatorConfig.Spec.UnsupportedConfigOverrides.Raw, operatorConfig.Spec.ObservedConfig.Raw)
	if err != nil {
		return nil, false, err
	}

	// we can embed input hashes on our main configmap to drive rollouts when they change.
	inputHashes, err := resourcehash.MultipleObjectHashStringMapForObjectReferences(
		kubeClient,
		resourcehash.NewObjectRef().ForConfigMap().InNamespace(targetNamespaceName).Named("client-ca"),
		resourcehash.NewObjectRef().ForSecret().InNamespace(targetNamespaceName).Named("serving-cert"),
	)
	if err != nil {
		return nil, false, err
	}
	for k, v := range inputHashes {
		requiredConfigMap.Data[k] = v
	}

	return resourceapply.ApplyConfigMap(client, recorder, requiredConfigMap)
}

func manageServiceCatalogControllerManagerTrustedCAConfigMap_v311_00_to_latest(kubeClient kubernetes.Interface, client coreclientv1.ConfigMapsGetter, recorder events.Recorder, operatorConfig *operatorapiv1.ServiceCatalogControllerManager) (*corev1.ConfigMap, bool, error) {
	trustedCAConfigMap := resourceread.ReadConfigMapV1OrDie(v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/trusted-ca.yaml"))

	currentTrustedCAConfigMap, err := client.ConfigMaps(targetNamespaceName).Get(trustedCABundle, metav1.GetOptions{})

	// Ensure we create the ConfigMap for the trusted-ca-bundle, and that it has
	// the right labels. Lifted from cluster-openshift-controller-manager
	// operator for the most part.
	if apierrors.IsNotFound(err) {
		newConfig, err := client.ConfigMaps(targetNamespaceName).Create(trustedCAConfigMap)
		if err != nil {
			recorder.Eventf("ConfigMapCreateFailed", "Failed to create %s/%s-n %s: %v", "configmap", "trusted-ca-bundle", targetNamespaceName, err)
			return nil, false, err
		}
		recorder.Eventf("ConfigMapCreated", "Created %s/%s-n %s because it was missing", "configmap", "trusted-ca-bundle", targetNamespaceName)
		return newConfig, true, nil
	} else if err != nil {
		return nil, false, err
	}

	// Ensure the trusted-ca-bundle ConfigMap has the correct label
	modified := resourcemerge.BoolPtr(false)
	currentCopy := currentTrustedCAConfigMap.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &currentCopy.ObjectMeta, trustedCAConfigMap.ObjectMeta)
	if !*modified {
		return currentTrustedCAConfigMap, false, nil
	}

	updated, err := client.ConfigMaps(targetNamespaceName).Update(currentCopy)
	if err != nil {
		recorder.Eventf("ConfigMapUpdateFailed", "Failed to update %s/%s-n %s: %v", "configmap", "trusted-ca-bundle", targetNamespaceName, err)
		return nil, false, err
	}
	recorder.Eventf("ConfigMapUpdated", "Updated %s/%s-n %s because it was missing", "configmap", "trusted-ca-bundle", targetNamespaceName)
	return updated, true, nil
}

func manageServiceCatalogControllerManagerDeployment_v311_00_to_latest(
	client appsclientv1.DaemonSetsGetter, recorder events.Recorder,
	options *operatorapiv1.ServiceCatalogControllerManager, imagePullSpec string,
	generationStatus []operatorapiv1.GenerationStatus, forceRollout bool,
	proxyConfig *configv1.Proxy, configMapClient coreclientv1.ConfigMapsGetter) (*appsv1.DaemonSet, bool, error) {

	// read the stock daemonset, this is NOT the live one
	required := resourceread.ReadDaemonSetV1OrDie(v311_00_assets.MustAsset("v3.11.0/openshift-svcat-controller-manager/ds.yaml"))

	if len(imagePullSpec) > 0 {
		required.Spec.Template.Spec.Containers[0].Image = imagePullSpec
	}

	level := 3
	switch options.Spec.LogLevel {
	case operatorapiv1.TraceAll:
		level = 8
	case operatorapiv1.Trace:
		level = 6
	case operatorapiv1.Debug:
		level = 4
	case operatorapiv1.Normal:
		level = 3
	}

	// if trustedCAConfigMap exists, we should add a mount point to the daemonset
	exists, err := trustedCAConfigMapExists(configMapClient, targetNamespaceName)
	if err != nil {
		return nil, false, err
	}
	if exists {
		addTrustedCAVolumeToDaemonSet(required)
	}

	// ================================================================

	var foundDaemonSet bool

	if proxyConfig != nil {
		existing, err := client.DaemonSets(required.Namespace).Get(required.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			foundDaemonSet = false

			klog.Info("we have a proxyConfig, but no existing daemonset, updating environment")
			// update the EnvVar
			addProxyToEnvironment(required, proxyConfig)

			// looks like this is the first time
			forceRollout = true
		} else if err != nil {
			foundDaemonSet = false
			klog.Error("Problem getting the daemonset, returning")
			return nil, false, err
		} else if err == nil {
			foundDaemonSet = true
		}

		if foundDaemonSet {
			if len(existing.Spec.Template.Spec.Containers) < 1 {
				klog.Error("We have a proxyConfig, but the existing daemonset has no defined containers")
				return nil, false, fmt.Errorf("the existing daemonset has no defined containers")
			}

			// if we have any environments, loop to find the envvars
			// if we detect a change, force the rollout
			for _, envVar := range existing.Spec.Template.Spec.Containers[0].Env {
				switch envVar.Name {
				case httpProxyEnvVar, strings.ToLower(httpProxyEnvVar):
					forceRollout = forceRollout || proxyConfig.Status.HTTPProxy != envVar.Value
					if proxyConfig.Status.HTTPProxy != envVar.Value {
						klog.Infof("httpproxy [%s] != [%s]; forceRollout %v", proxyConfig.Status.HTTPProxy, envVar.Value, forceRollout)
					}
				case httpsProxyEnvVar, strings.ToLower(httpsProxyEnvVar):
					forceRollout = forceRollout || proxyConfig.Status.HTTPSProxy != envVar.Value
					if proxyConfig.Status.HTTPSProxy != envVar.Value {
						klog.Infof("httpsproxy [%s] != [%s]; forceRollout %v", proxyConfig.Status.HTTPSProxy, envVar.Value, forceRollout)
					}
				case noProxyEnvVar, strings.ToLower(noProxyEnvVar):
					forceRollout = forceRollout || proxyConfig.Status.NoProxy != envVar.Value
					if proxyConfig.Status.NoProxy != envVar.Value {
						klog.Infof("noproxy [%s] != [%s]; forceRollout %v", proxyConfig.Status.NoProxy, envVar.Value, forceRollout)
					}
				default:
					klog.Infof("None of the cases matched. forceRollout: %v", forceRollout)
				}
			}

			// if we detected a change, forceRollout will be set, update
			// environment
			if forceRollout {
				klog.Info("we have a proxyConfig and a daemonset, we detected a change in the env, updating required env")
				// update the EnvVar
				addProxyToEnvironment(required, proxyConfig)
			}
		}
	} else if proxyConfig == nil {
		// use required, do not add environment, or make it empty
		existing, err := client.DaemonSets(required.Namespace).Get(required.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// do nothing
			foundDaemonSet = false
		} else if err != nil {
			foundDaemonSet = false
			klog.Error("Problem getting the daemonset, returning")
			return nil, false, err
		} else if err == nil {
			foundDaemonSet = true
		}

		// need to blank out EnvVar
		if foundDaemonSet {
			if len(existing.Spec.Template.Spec.Containers) < 1 {
				klog.Error("We have no proxyConfig, but the existing daemonset has no defined containers")
				return nil, false, fmt.Errorf("the existing daemonset has no defined containers")
			}

			// see if there was a proxy that needs to get unset
			for _, envVar := range existing.Spec.Template.Spec.Containers[0].Env {
				switch envVar.Name {
				case httpProxyEnvVar, strings.ToLower(httpProxyEnvVar),
					httpsProxyEnvVar, strings.ToLower(httpsProxyEnvVar),
					noProxyEnvVar, strings.ToLower(noProxyEnvVar):

					forceRollout = forceRollout || len(envVar.Value) > 0
					if len(envVar.Value) > 0 {
						klog.Infof("%s [%s] != ['']; forceRollout %v", envVar.Name, envVar.Value, forceRollout)
					}
				default:
					klog.Infof("None of the cases matched. forceRollout unchanged: %v", forceRollout)
				}
			}
			// if we detected a change, forceRollout will be set, update
			// environment
			if forceRollout {
				klog.Info("we have no proxyConfig but we do have a daemonset, we detected a change in the env, updating required env")
			}
		}
	}

	// ================================================================

	required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", level))
	if required.Annotations == nil {
		required.Annotations = map[string]string{}
	}
	required.Annotations[util.VersionAnnotation] = os.Getenv("RELEASE_VERSION")

	return resourceapply.ApplyDaemonSet(client, recorder, required, resourcemerge.ExpectedDaemonSetGeneration(required, generationStatus), forceRollout)
}

func addTrustedCAVolumeToDaemonSet(required *appsv1.DaemonSet) {
	// volumeMount:
	//   - mountPath: /etc/pki/ca-trust/extracted/pem/
	//     name: trusted-ca-bundle
	// volumes:
	//   - name: trusted-ca-bundle
	//     configMap:
	//       name: trusted-ca-bundle
	//       items:
	//       - key: ca-bundle.crt
	//         path: "tls-ca-bundle.pem"

	required.Spec.Template.Spec.Containers[0].VolumeMounts = append(
		required.Spec.Template.Spec.Containers[0].VolumeMounts,

		corev1.VolumeMount{
			Name:      trustedCABundle,
			MountPath: "/etc/pki/ca-trust/extracted/pem/",
		})

	optionalVolume := true
	required.Spec.Template.Spec.Volumes = append(required.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: trustedCABundle,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: trustedCABundle,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "ca-bundle.crt",
							Path: "tls-ca-bundle.pem",
						},
					},
					Optional: &optionalVolume,
				},
			},
		})
}

func addProxyToEnvironment(required *appsv1.DaemonSet, proxyConfig *configv1.Proxy) {
	required.Spec.Template.Spec.Containers[0].Env = append(required.Spec.Template.Spec.Containers[0].Env,
		[]corev1.EnvVar{
			{
				Name:  httpProxyEnvVar,
				Value: proxyConfig.Status.HTTPProxy,
			},
			{
				Name:  httpsProxyEnvVar,
				Value: proxyConfig.Status.HTTPSProxy,
			},
			{
				Name:  noProxyEnvVar,
				Value: proxyConfig.Status.NoProxy,
			},
			{
				Name:  strings.ToLower(httpProxyEnvVar),
				Value: proxyConfig.Status.HTTPProxy,
			},
			{
				Name:  strings.ToLower(httpsProxyEnvVar),
				Value: proxyConfig.Status.HTTPSProxy,
			},
			{
				Name:  strings.ToLower(noProxyEnvVar),
				Value: proxyConfig.Status.NoProxy,
			},
		}...)
}

func trustedCAConfigMapExists(client coreclientv1.ConfigMapsGetter, namespace string) (bool, error) {
	_, err := client.ConfigMaps(namespace).Get(trustedCABundle, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	// configmap found
	return true, nil
}
