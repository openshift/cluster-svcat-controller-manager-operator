# cluster-svcat-controller-manager-operator
The svcat-controller-manager operator installs and maintains the Service Catalog controller-manager on a cluster.  This operator only deals with the Controller Manager portion of Service Catalog; see also the `cluster-svcat-apiserver-operator`.

Note that the manifests do not create the Cluster Operator or the ServiceCatalogControllerManager custom resource.  While the CVO installs the Service Catalog operators, we don't want Service Catalog installed by default.  The cluster admin must create the ServiceCatalogControllerManager CR to cause the operator to perform the installation ([see below](#Trigger-installation-of-Service-Catalog-API-Server))

Once the operator detects the CR it will create the Service Catalog Controller Manager Cluster Operator resource and proceed with reconciling the Service Catalog Controller Manager deployment.

## Deployment the operator prior to CVO integration
1. Use openshift/installer to install a cluster.  Skip to step 6 if you want to use pre-built operator images.
2. `make images`
3. `docker tag openshift/origin-cluster-svcat-controller-manager-operator:latest <yourdockerhubid>/origin-cluster-svcat-controller-manager-operator:latest`
4. `docker push <yourdockerhubid>/origin-cluster-svcat-controller-manager-operator:latest`
5. edit manifests/0000_63_svcat-controller-manager-operator_09_deployment.yaml and update the containers/image to `<yourdockerhubid>/origin-cluster-svcat-controller-manager-operator:latest` and set the pull policy to `Always`
6.  `oc apply -f manifests`

This will cause the creation of the cluster-svcat-controller-manager-operator deployment 
and associated resources.  The operator waits for creation of the `ServiceCatalogControllerManager`
custom resource before doing any real work including creating the Cluster Operator `openshift-svcat-controller-manager`.

## Trigger installation of Service Catalog API Server
Create the `ServiceCatalogControllerManager` CR to trigger the installation of Service Catalog:
```
$ cat <<'EOF' | oc create -f -
apiVersion: operator.openshift.io/v1
kind: ServiceCatalogControllerManager
metadata:
  name: cluster
spec:
  managementState: Managed
EOF
```
Once the cluster `ServiceCatalogControllerManager` is found to exist and have a `managementState` of `Managed` the operator will create necessary resources in the
`openshift-service-catalog-controller-manager` namespace for deploying the Service Catalog API Server.

Watch for service catalog controller manager to come up in the openshift-service-catalog-controller-manager namespace.

## Verification & debugging
Nothing happens without the CR:
```
$ oc get servicecatalogcontrollermanagers
NAME      AGE
cluster     10m
```
If the state is `Managed` the operator will install Service Catalog Controller Manager.  You can remove the deployment by setting the state to `Removed`.

Once the CR is created the operator should create a new ClusterOperator resource:
```
oc get clusteroperator openshift-svcat-controller-manager
NAME                        VERSION   AVAILABLE   PROGRESSING   FAILING   SINCE
svcat-controller-manager              True        False         False     10m

```
Review operator pod logs from the `openshift-service-catalog-controller-manager-operator` namespace to see details of the operator processing.


The operator deployment events will give you an overview of what it's done.  Ensure its not looping & review the events:
```
$ oc describe deployment openshift-service-catalog-controller-manager-operator -n openshift-service-catalog-controller-manager-operator
```





