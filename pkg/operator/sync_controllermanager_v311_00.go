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
)

// syncServiceCatalogControllerManager_v311_00_to_latest takes care of synchronizing (not upgrading) the thing we're managing.
// most of the time the sync method will be good for a large span of minor versions
func syncServiceCatalogControllerManager_v311_00_to_latest(c ServiceCatalogControllerManagerOperator, originalOperatorConfig *operatorapiv1.ServiceCatalogControllerManager, proxyConfig *configv1.Proxy) (bool, error) {
	errors := []error{}
	var err error
	operatorConfig := originalOperatorConfig.DeepCopy()
	directResourceResults := resourceapply.ApplyDirectly(c.kubeClient, c.recorder, v311_00_assets.Asset,
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
	// the kube-apiserver is the source of truth for client CA bundles
	clientCAModified, err := manageServiceCatalogControllerManagerClientCA_v311_00_to_latest(c.kubeClient.CoreV1(), c.recorder)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q: %v", "client-ca", err))
	}

	forceRollout = forceRollout || operatorConfig.ObjectMeta.Generation != operatorConfig.Status.ObservedGeneration
	forceRollout = forceRollout || configMapModified || clientCAModified

	/*
		// verify proxy config hasn't changed
		klog.Errorf("http before %s after %s", proxyConfig.Spec.HTTPProxy, proxyConfig.Status.HTTPProxy)
		klog.Errorf("https before %s after %s", proxyConfig.Spec.HTTPSProxy, proxyConfig.Status.HTTPSProxy)
		klog.Errorf("noproxy before %s after %s", proxyConfig.Spec.NoProxy, proxyConfig.Status.NoProxy)

		var foorollout bool
		foorollout = foorollout || proxyConfig.Spec.HTTPProxy != proxyConfig.Status.HTTPProxy
		foorollout = foorollout || proxyConfig.Spec.HTTPSProxy != proxyConfig.Status.HTTPSProxy
		foorollout = foorollout || proxyConfig.Spec.NoProxy != proxyConfig.Status.NoProxy
		klog.Errorf("forceRollout would be set to %v", foorollout)
	*/

	// our configmaps and secrets are in order, now it is time to create the DS
	// TODO check basic preconditions here
	actualDaemonSet, _, err := manageServiceCatalogControllerManagerDeployment_v311_00_to_latest(c.kubeClient.AppsV1(), c.recorder, operatorConfig, c.targetImagePullSpec, operatorConfig.Status.Generations, forceRollout, proxyConfig)
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

func manageServiceCatalogControllerManagerDeployment_v311_00_to_latest(
	client appsclientv1.DaemonSetsGetter, recorder events.Recorder,
	options *operatorapiv1.ServiceCatalogControllerManager, imagePullSpec string,
	generationStatus []operatorapiv1.GenerationStatus, forceRollout bool,
	proxyConfig *configv1.Proxy) (*appsv1.DaemonSet, bool, error) {

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
		level = 4
	}

	// get the real daemonset so we can check to see if the proxy from the
	// environment has changed or not.
	existing, err := client.DaemonSets(required.Namespace).Get(required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// we probably don't want to create it, we just want to add the proxy
		// stuff to the Env of required
		// actual, err := client.DaemonSets(required.Namespace).Create(required)
		// reportCreateEvent(recorder, required, err)
		// return actual, true, err
		klog.Errorf("XXX daemonset not found")
		if proxyConfig != nil {
			klog.Infof("XXX we have a proxy config, and no daemonset, adding config to required")
			// update the EnvVar
			required.Spec.Template.Spec.Containers[0].Env = append(required.Spec.Template.Spec.Containers[0].Env,
				[]corev1.EnvVar{
					{
						Name:  httpProxyEnvVar,
						Value: proxyConfig.Spec.HTTPProxy,
					},
					{
						Name:  httpsProxyEnvVar,
						Value: proxyConfig.Spec.HTTPSProxy,
					},
					{
						Name:  noProxyEnvVar,
						Value: proxyConfig.Spec.NoProxy,
					},
					{
						Name:  strings.ToLower(httpProxyEnvVar),
						Value: proxyConfig.Spec.HTTPProxy,
					},
					{
						Name:  strings.ToLower(httpsProxyEnvVar),
						Value: proxyConfig.Spec.HTTPSProxy,
					},
					{
						Name:  strings.ToLower(noProxyEnvVar),
						Value: proxyConfig.Spec.NoProxy,
					},
				}...)
			forceRollout = true
		}
	} else if err != nil {
		klog.Error("XXX Problem getting daemonset")
		return nil, false, err
	}

	if proxyConfig == nil {
		klog.Error("XXX proxyConfig is nil")
	} else if proxyConfig != nil {
		klog.Errorf("XXX proxyConfig is %v", proxyConfig)
	}
	klog.Errorf("XXX Environment length is %d", len(existing.Spec.Template.Spec.Containers[0].Env))

	// if there's no proxyconfig, we want to replace the environment anyway
	if proxyConfig == nil && len(existing.Spec.Template.Spec.Containers[0].Env) > 0 {
		klog.Infof("XXX no proxyConfig, but environment exists, forcing rollout!")
		required.Spec.Template.Spec.Containers[0].Env = append(required.Spec.Template.Spec.Containers[0].Env, []corev1.EnvVar{}...)
		forceRollout = true
	} else if proxyConfig == nil { // an no environment
		// no environment, do nothing
		klog.Infof("XXX no proxyConfig and no environment")
	} else if proxyConfig != nil {
		klog.Infof("XXX we have a proxyConfig")
		klog.Infof("Proxy Config supplied, using %#v\n", proxyConfig)

		// if we have any environments, loop to find the envvars
		// if we detect a change, force the rollout
		for _, envVar := range existing.Spec.Template.Spec.Containers[0].Env {
			switch envVar.Name {
			case httpProxyEnvVar, strings.ToLower(httpProxyEnvVar):
				forceRollout = forceRollout || proxyConfig.Spec.HTTPProxy != envVar.Value
				klog.Infof("proxy %s != %s; forceRollout %v", proxyConfig.Spec.HTTPProxy, envVar.Value, forceRollout)
			case httpsProxyEnvVar, strings.ToLower(httpsProxyEnvVar):
				forceRollout = forceRollout || proxyConfig.Spec.HTTPSProxy != envVar.Value
				klog.Infof("proxy %s != %s; forceRollout %v", proxyConfig.Spec.HTTPSProxy, envVar.Value, forceRollout)
			case noProxyEnvVar, strings.ToLower(noProxyEnvVar):
				forceRollout = forceRollout || proxyConfig.Spec.NoProxy != envVar.Value
				klog.Infof("proxy %s != %s; forceRollout %v", proxyConfig.Spec.NoProxy, envVar.Value, forceRollout)
			default:
				klog.Infof("None of the cases matched. forceRollout: %v", forceRollout)
			}
		}

		// if len(existing.Spec.Template.Spec.Containers[0].Env) < 1 {
		//     klog.Infof("XXX no environment to update")
		//     // no environment to update, see if the proxy has anything in it
		//     forceRollout = forceRollout || proxyConfig.Spec.HTTPProxy != ""
		//     forceRollout = forceRollout || proxyConfig.Spec.HTTPSProxy != ""
		//     forceRollout = forceRollout || proxyConfig.Spec.NoProxy != ""
		// }

		if forceRollout {
			klog.Infof("XXX we have a proxy config and are forcing the rollout")
			// update the EnvVar
			required.Spec.Template.Spec.Containers[0].Env = append(required.Spec.Template.Spec.Containers[0].Env,
				[]corev1.EnvVar{
					{
						Name:  httpProxyEnvVar,
						Value: proxyConfig.Spec.HTTPProxy,
					},
					{
						Name:  httpsProxyEnvVar,
						Value: proxyConfig.Spec.HTTPSProxy,
					},
					{
						Name:  noProxyEnvVar,
						Value: proxyConfig.Spec.NoProxy,
					},
					{
						Name:  strings.ToLower(httpProxyEnvVar),
						Value: proxyConfig.Spec.HTTPProxy,
					},
					{
						Name:  strings.ToLower(httpsProxyEnvVar),
						Value: proxyConfig.Spec.HTTPSProxy,
					},
					{
						Name:  strings.ToLower(noProxyEnvVar),
						Value: proxyConfig.Spec.NoProxy,
					},
				}...)
		}
	}

	klog.Infof("managedSvcatCMDeployment: Env [%#v]", required.Spec.Template.Spec.Containers[0].Env)

	required.Spec.Template.Spec.Containers[0].Args = append(required.Spec.Template.Spec.Containers[0].Args, fmt.Sprintf("-v=%d", level))
	if required.Annotations == nil {
		required.Annotations = map[string]string{}
	}
	required.Annotations[util.VersionAnnotation] = os.Getenv("RELEASE_VERSION")

	return resourceapply.ApplyDaemonSet(client, recorder, required, resourcemerge.ExpectedDaemonSetGeneration(required, generationStatus), forceRollout)
}
