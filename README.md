# cluster-svcat-controller-manager-operator
The cluster-svcat-controller-manager-operator installs and maintains a singleton instance of the OpenShift Service Catalog on a cluster.  Service Catalog is actually comprised of an aggregated API server and a controller manager; this operator only deals with the Controller Manager portion of Service Catalog.  See the [cluster-svcat-apiserver-operator](https://github.com/openshift/cluster-svcat-apiserver-operator) for the operator responsible for the API Server component of Service Catalog.

It should be noted this repo was initially copied from the OpenShift Controller Manager Operator (https://github.com/openshift/cluster-openshift-controller-manager-operator) and we generally try to keep it in sync with fixes and updates that are applicable.

[The Cluster Version Operator](https://github.com/openshift/cluster-version-operator) installs cluster operators by collecting the files within each cluster operator's manifest directory, bundling them into a release payload, and then `oc apply`ing them.  Note that unlike most cluster operators, this operator's configuration specifies that the initial management state of the Operator is `Removed`.  That is, the cluster operator is installed and running, but the operand is not.

This operator is installed to the `openshift-service-catalog-controller-manager-operator` namespace.  It installs the Service Catalog Controller Manager into the `openshift-service-catalog-controller-manager` namespace.  In prior versions, both the Service Catalog API Server and Controller Manager were installed to kube-service-catalog.  This change keeps with how the OpenShift API Server & Controller Manager are managed and makes some aspects of servicability easier.


## Installing Service Catalog
To enable and install Service Catalog, the cluster admin must modify two Service Catalog custom resources and change the `ManagementState` to `Managed`. 
```
$ oc edit ServiceCatalogAPIServer
```
locate the `managementState` and change `Removed` to `Managed`.  Repeat for the controller-manager:
```
$ oc edit ServiceCatalogControllerManager
```
note the first resource is actually from the [cluster-svcat-apiserver-operator](https://github.com/openshift/cluster-svcat-apiserver-operator).  The controller-manager operator will see the change in the desired state and create necessary resources in the `openshift-service-catalog-controller-manager` namespace for deploying the Service Catalog Controller Manager.


## Verification & debugging
Review the cluster operator status, it should report `Available` if it is in the desired state.  Although a bit contrary to the notion of "available", it should be pointed out that when the managementState is `Removed` and Service Catalog is not installed, the operator should be reporting Available=true because it is in the desired state.
```
$ oc get clusteroperators service-catalog-controller-manager
NAME                        VERSION                        AVAILABLE   PROGRESSING   DEGRADED   SINCE
service-catalog-controller-manager   4.1.0-0.ci-2019-05-01-061138   True        False         False      3m57s
```
View the operator pod logs:
```
$ oc logs deployment/openshift-service-catalog-controller-manager-operator -n openshift-service-catalog-controller-manager-operator
```
The events present a good summary of actions the operator has taken to reach the desired state:
```
$ oc get events --sort-by='.lastTimestamp'  -n openshift-service-catalog-controller-manager-operator
```

If the state is `Managed` the operator will install Service Catalog API Server.  You can request the Service Catalog deployment to be removed by setting the state to `Removed`.  

## Hacking with your own Operator or Operand
You can make changes to the operator and deploy it to your cluster.  First you disable the CVO so it doesn't overwrite your changes from what is in the release payload:
```
$ oc scale --replicas 0 -n openshift-cluster-version deployments/cluster-version-operator
```
this is a big hammer, you could instead just tell the CVO your operator should be unmanged, see [Setting Objects unmanaged](https://github.com/openshift/cluster-version-operator/blob/master/docs/dev/clusterversion.md#setting-objects-unmanaged)

Build and push your newly built image to a repo:
```
$ make image
$ docker tag openshift/origin-cluster-svcat-controller-manager-operator:latest jboyd01/origin-cluster-svcat-controller-manager-operator:xx
$ docker push jboyd01/origin-cluster-svcat-controller-manager-operator:xx
```
and then update the manifest to specify your operator  image:
```
$ oc edit deployment -n openshift-service-catalog-controller-manager-operator
```
locate the image and change the image and pull policy:
```
        image: registry.svc.ci.openshift.org/ocp/4.1-2019-05-01-061138@sha256:de5e1c8a2605f75b71705a933c31f4dff3ff1ae860d7a86d771dbe2043a4cea0
        imagePullPolicy: IfNotPresent
```
to
```
        image: docker.io/jboyd01/origin-cluster-svcat-controller-manager-operator:xx
        imagePullPolicy: Always
```
This will cause your dev operator image to be pulled down and deployed.  When you want to deploy a newly built image just scale your operator to zero and right back to one:
```
$ oc scale --replicas 0 -n openshift-service-catalog-controller-manager-operator deployments/openshift-service-catalog-controller-manager-operator
$ oc scale --replicas 1 -n openshift-service-catalog-controller-manager-operator deployments/openshift-service-catalog-controller-manager-operator
```

If you want your own Service Catalog API Server to be deployed you follow a simlar process but instead update the deployment's IMAGE environment variable:
```
        env:
        - name: IMAGE
          value: registry.svc.ci.openshift.org/ocp/4.1-2019-05-01-061138@sha256:cc22f2af68a261c938fb1ec9a9e94643eba23a3bb8c9e22652139f80ee57681b
```
and change the value to your own repo, something like
```
        env:
        - name: IMAGE
          value: docker.io/jboyd01/service-catalog:latest
```
## Read about the CVO if you haven't yet
Consider this required reading - its vital to understanding how the operator should work and why:
* https://github.com/openshift/cluster-version-operator#cluster-version-operator-cvo
* https://github.com/openshift/cluster-version-operator/tree/master/docs/dev

## Other development notes
If you make changes to the yaml resources under `bindata` you must run the script `hack/update-generated-bindata.sh` to update the go source files which are responsible for creating the Service Catalog operand deployment resources.

When picking up new versions of dependencies, use the script `hack/update-deps.sh`.  Generally you want to mirror the `glide.yaml` and dependency updates driven from the OpenShift controller-manager operator.
