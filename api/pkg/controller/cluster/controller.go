package cluster

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/metrics"
	"github.com/golang/glog"
	apiv1 "github.com/kubermatic/kubermatic/api/pkg/api/v1"
	"github.com/kubermatic/kubermatic/api/pkg/controller/version"
	kubermaticclientset "github.com/kubermatic/kubermatic/api/pkg/crd/client/clientset/versioned"
	etcdoperatorv1beta2informers "github.com/kubermatic/kubermatic/api/pkg/crd/client/informers/externalversions/etcdoperator/v1beta2"
	kubermaticv1informers "github.com/kubermatic/kubermatic/api/pkg/crd/client/informers/externalversions/kubermatic/v1"
	etcdoperatorv1beta2lister "github.com/kubermatic/kubermatic/api/pkg/crd/client/listers/etcdoperator/v1beta2"
	kubermaticv1lister "github.com/kubermatic/kubermatic/api/pkg/crd/client/listers/kubermatic/v1"
	kubermaticv1 "github.com/kubermatic/kubermatic/api/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/api/pkg/provider"
	machineclientset "github.com/kubermatic/machine-controller/pkg/client/clientset/versioned"
	appsv1informer "k8s.io/client-go/informers/apps/v1"
	rbacv1informer "k8s.io/client-go/informers/rbac/v1"

	kubeapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	extensionsv1beta1informers "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	appsv1lister "k8s.io/client-go/listers/apps/v1"
	corev1lister "k8s.io/client-go/listers/core/v1"
	extensionsv1beta1lister "k8s.io/client-go/listers/extensions/v1beta1"
	rbacb1lister "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	validatingSyncPeriod = 15 * time.Second
	launchingSyncPeriod  = 2 * time.Second
	deletingSyncPeriod   = 10 * time.Second
	runningSyncPeriod    = 60 * time.Second
)

// UserClusterConnectionProvider offers functions to retrieve clients for the given user clusters
type UserClusterConnectionProvider interface {
	GetClient(*kubermaticv1.Cluster) (kubernetes.Interface, error)
	GetMachineClient(*kubermaticv1.Cluster) (machineclientset.Interface, error)
}

// Controller is a controller which is responsible for managing clusters
type Controller struct {
	kubermaticClient        kubermaticclientset.Interface
	kubeClient              kubernetes.Interface
	userClusterConnProvider UserClusterConnectionProvider

	masterResourcesPath string
	externalURL         string
	dcs                 map[string]provider.DatacenterMeta
	dc                  string
	cps                 map[string]provider.CloudProvider

	queue      workqueue.RateLimitingInterface
	workerName string

	versions              map[string]*apiv1.MasterVersion
	updates               []apiv1.MasterUpdate
	defaultMasterVersion  *apiv1.MasterVersion
	automaticUpdateSearch *version.UpdatePathSearch
	overwriteRegistry     string
	nodePortRange         string

	metrics ControllerMetrics

	clusterLister            kubermaticv1lister.ClusterLister
	clusterSynced            cache.InformerSynced
	etcdClusterLister        etcdoperatorv1beta2lister.EtcdClusterLister
	etcdClusterSynced        cache.InformerSynced
	namespaceLister          corev1lister.NamespaceLister
	namespaceSynced          cache.InformerSynced
	secretLister             corev1lister.SecretLister
	secretSynced             cache.InformerSynced
	serviceLister            corev1lister.ServiceLister
	serviceSynced            cache.InformerSynced
	pvcLister                corev1lister.PersistentVolumeClaimLister
	pvcSynced                cache.InformerSynced
	configMapLister          corev1lister.ConfigMapLister
	configMapSynced          cache.InformerSynced
	serviceAccountLister     corev1lister.ServiceAccountLister
	serviceAccountSynced     cache.InformerSynced
	deploymentLister         appsv1lister.DeploymentLister
	deploymentSynced         cache.InformerSynced
	statefulSetLister        appsv1lister.StatefulSetLister
	statefulSynced           cache.InformerSynced
	ingressLister            extensionsv1beta1lister.IngressLister
	ingressSynced            cache.InformerSynced
	roleLister               rbacb1lister.RoleLister
	roleSynced               cache.InformerSynced
	roleBindingLister        rbacb1lister.RoleBindingLister
	roleBindingSynced        cache.InformerSynced
	clusterRoleBindingLister rbacb1lister.ClusterRoleBindingLister
	clusterRoleBindingSynced cache.InformerSynced
}

// ControllerMetrics contains metrics about the clusters & workers
type ControllerMetrics struct {
	Clusters        metrics.Gauge
	ClusterPhases   metrics.Gauge
	Workers         metrics.Gauge
	UnhandledErrors metrics.Counter
}

// NewController creates a cluster controller.
func NewController(
	kubeClient kubernetes.Interface,
	kubermaticClient kubermaticclientset.Interface,
	versions map[string]*apiv1.MasterVersion,
	updates []apiv1.MasterUpdate,
	masterResourcesPath string,
	externalURL string,
	workerName string,
	dc string,
	dcs map[string]provider.DatacenterMeta,
	cps map[string]provider.CloudProvider,
	metrics ControllerMetrics,
	userClusterConnProvider UserClusterConnectionProvider,
	overwriteRegistry string,
	nodePortRange string,

	clusterInformer kubermaticv1informers.ClusterInformer,
	etcdClusterInformer etcdoperatorv1beta2informers.EtcdClusterInformer,
	namespaceInformer corev1informers.NamespaceInformer,
	secretInformer corev1informers.SecretInformer,
	serviceInformer corev1informers.ServiceInformer,
	pvcInformer corev1informers.PersistentVolumeClaimInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	serviceAccountInformer corev1informers.ServiceAccountInformer,
	deploymentInformer appsv1informer.DeploymentInformer,
	statefulSetInformer appsv1informer.StatefulSetInformer,
	ingressInformer extensionsv1beta1informers.IngressInformer,
	roleInformer rbacv1informer.RoleInformer,
	roleBindingInformer rbacv1informer.RoleBindingInformer,
	clusterRoleBindingInformer rbacv1informer.ClusterRoleBindingInformer) (*Controller, error) {
	cc := &Controller{
		kubermaticClient:        kubermaticClient,
		kubeClient:              kubeClient,
		userClusterConnProvider: userClusterConnProvider,

		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cluster"),

		updates:           updates,
		versions:          versions,
		overwriteRegistry: overwriteRegistry,
		nodePortRange:     nodePortRange,

		masterResourcesPath: masterResourcesPath,
		externalURL:         externalURL,
		workerName:          workerName,
		dc:                  dc,
		dcs:                 dcs,
		cps:                 cps,
		metrics:             metrics,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cc.enqueue(obj.(*kubermaticv1.Cluster))
		},
		UpdateFunc: func(old, cur interface{}) {
			cc.enqueue(cur.(*kubermaticv1.Cluster))
		},
		DeleteFunc: func(obj interface{}) {
			cluster, ok := obj.(*kubermaticv1.Cluster)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					runtime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
					return
				}
				cluster, ok = tombstone.Obj.(*kubermaticv1.Cluster)
				if !ok {
					runtime.HandleError(fmt.Errorf("tombstone contained object that is not a Cluster %#v", obj))
					return
				}
			}
			cc.enqueue(cluster)
		},
	})

	//In case one of our child objects change, we should update our state
	namespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	pvcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	configMapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	serviceAccountInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	roleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	roleBindingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	clusterRoleBindingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})
	etcdClusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cc.handleChildObject(obj) },
		UpdateFunc: func(old, cur interface{}) { cc.handleChildObject(cur) },
		DeleteFunc: func(obj interface{}) { cc.handleChildObject(obj) },
	})

	cc.clusterLister = clusterInformer.Lister()
	cc.clusterSynced = clusterInformer.Informer().HasSynced
	cc.etcdClusterLister = etcdClusterInformer.Lister()
	cc.etcdClusterSynced = etcdClusterInformer.Informer().HasSynced
	cc.namespaceLister = namespaceInformer.Lister()
	cc.namespaceSynced = namespaceInformer.Informer().HasSynced
	cc.secretLister = secretInformer.Lister()
	cc.secretSynced = secretInformer.Informer().HasSynced
	cc.serviceLister = serviceInformer.Lister()
	cc.serviceSynced = serviceInformer.Informer().HasSynced
	cc.pvcLister = pvcInformer.Lister()
	cc.pvcSynced = pvcInformer.Informer().HasSynced
	cc.configMapLister = configMapInformer.Lister()
	cc.configMapSynced = configMapInformer.Informer().HasSynced
	cc.serviceAccountLister = serviceAccountInformer.Lister()
	cc.serviceAccountSynced = serviceAccountInformer.Informer().HasSynced
	cc.deploymentLister = deploymentInformer.Lister()
	cc.deploymentSynced = deploymentInformer.Informer().HasSynced
	cc.statefulSetLister = statefulSetInformer.Lister()
	cc.statefulSynced = statefulSetInformer.Informer().HasSynced
	cc.ingressLister = ingressInformer.Lister()
	cc.ingressSynced = ingressInformer.Informer().HasSynced
	cc.roleLister = roleInformer.Lister()
	cc.roleSynced = roleInformer.Informer().HasSynced
	cc.roleBindingLister = roleBindingInformer.Lister()
	cc.roleBindingSynced = roleBindingInformer.Informer().HasSynced
	cc.clusterRoleBindingLister = clusterRoleBindingInformer.Lister()
	cc.clusterRoleBindingSynced = clusterRoleBindingInformer.Informer().HasSynced

	var err error
	cc.defaultMasterVersion, err = version.DefaultMasterVersion(versions)
	if err != nil {
		return nil, fmt.Errorf("could not get default master version: %v", err)
	}

	var automaticUpdates []apiv1.MasterUpdate
	for _, u := range cc.updates {
		if u.Automatic {
			automaticUpdates = append(automaticUpdates, u)
		}
	}
	cc.automaticUpdateSearch = version.NewUpdatePathSearch(cc.versions, automaticUpdates, version.SemverMatcher{})

	// register error handler that will increment a counter that will be scraped by prometheus,
	// that accounts for all errors reported via a call to runtime.HandleError
	runtime.ErrorHandlers = append(runtime.ErrorHandlers, func(err error) {
		metrics.UnhandledErrors.Add(1.0)
	})

	return cc, nil
}

func (cc *Controller) enqueue(cluster *kubermaticv1.Cluster) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(cluster)
	if err != nil {
		runtime.HandleError(fmt.Errorf("couldn't get key for object %#v: %v", cluster, err))
		return
	}

	cc.queue.Add(key)
}

func (cc *Controller) updateCluster(originalData []byte, modifiedCluster *kubermaticv1.Cluster) error {
	currentCluster, err := cc.clusterLister.Get(modifiedCluster.Name)
	if err != nil {
		return err
	}

	currentData, err := json.Marshal(currentCluster)
	if err != nil {
		return err
	}

	modifiedData, err := json.Marshal(modifiedCluster)
	if err != nil {
		return err
	}

	patchData, err := jsonmergepatch.CreateThreeWayJSONMergePatch(originalData, modifiedData, currentData)
	if err != nil {
		return err
	}
	//Avoid empty patch calls
	if string(patchData) == "{}" {
		return nil
	}

	updatedCluster, err := cc.kubermaticClient.KubermaticV1().Clusters().Patch(modifiedCluster.Name, types.MergePatchType, patchData)
	if err != nil {
		return fmt.Errorf("failed to patch cluster: %v", err)
	}

	return wait.Poll(10*time.Millisecond, 30*time.Second, func() (bool, error) {
		listerCluster, err := cc.clusterLister.Get(updatedCluster.Name)
		if err != nil {
			// In case we remove the last finalizer, the object will be gone after the patch
			if kubeapierrors.IsNotFound(err) {
				return true, nil
			}
			runtime.HandleError(fmt.Errorf("failed to get cluster %s from lister during cache-update check: %v", updatedCluster.Name, err))
			return false, nil
		}
		if listerCluster.ResourceVersion == updatedCluster.ResourceVersion {
			return true, nil
		}
		return false, nil
	})
}

func (cc *Controller) updateClusterError(cluster *kubermaticv1.Cluster, reason kubermaticv1.ClusterStatusError, message string, originalData []byte) error {
	if cluster.Status.ErrorReason == nil || *cluster.Status.ErrorReason == reason {
		cluster.Status.ErrorMessage = &message
		cluster.Status.ErrorReason = &reason
		return cc.updateCluster(originalData, cluster)
	}
	return nil
}

func (cc *Controller) syncCluster(key string) error {
	listerCluster, err := cc.clusterLister.Get(key)
	if err != nil {
		if kubeapierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("unable to retrieve cluster %q: %v", key, err)
	}

	cluster := listerCluster.DeepCopy()
	originalData, err := json.Marshal(cluster)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster %s: %v", key, err)
	}

	if cluster.Spec.Pause {
		glog.V(6).Infof("skipping cluster %s due to it was set to paused", key)
		return nil
	}

	if cluster.Labels[kubermaticv1.WorkerNameLabelKey] != cc.workerName {
		glog.V(8).Infof("skipping cluster %s due to different worker assigned to it", key)
		return nil
	}

	glog.V(4).Infof("syncing cluster %s", key)

	for _, phase := range kubermaticv1.ClusterPhases {
		value := 0.0
		if phase == cluster.Status.Phase {
			value = 1.0
		}
		cc.metrics.ClusterPhases.With(
			"cluster", cluster.Name,
			"phase", strings.ToLower(string(phase)),
		).Set(value)
	}

	if cluster.DeletionTimestamp != nil {
		cluster.Status.Phase = kubermaticv1.DeletingClusterStatusPhase
		if err := cc.cleanupCluster(cluster); err != nil {
			return err
		}
		return cc.updateCluster(originalData, cluster)
	}

	if cluster.Status.Phase == kubermaticv1.NoneClusterStatusPhase {
		cluster.Status.Phase = kubermaticv1.ValidatingClusterStatusPhase
	}
	var updateErr error
	if err = cc.validateCluster(cluster); err != nil {
		updateErr = cc.updateClusterError(cluster, kubermaticv1.InvalidConfigurationClusterError, err.Error(), originalData)
		if updateErr != nil {
			return fmt.Errorf("failed to set the cluster error: %v", updateErr)
		}
		return err
	}

	if cluster.Status.Phase == kubermaticv1.ValidatingClusterStatusPhase {
		cluster.Status.Phase = kubermaticv1.LaunchingClusterStatusPhase
	}
	if err := cc.reconcileCluster(cluster); err != nil {
		updateErr = cc.updateClusterError(cluster, kubermaticv1.ReconcileClusterError, err.Error(), originalData)
		if updateErr != nil {
			return fmt.Errorf("failed to set the cluster error: %v", updateErr)
		}
		return err
	}

	return cc.updateCluster(originalData, cluster)
}

func (cc *Controller) runWorker() {
	for cc.processNextItem() {
	}
}

func (cc *Controller) processNextItem() bool {
	key, quit := cc.queue.Get()
	if quit {
		return false
	}

	defer cc.queue.Done(key)

	err := cc.syncCluster(key.(string))

	cc.handleErr(err, key)
	return true
}

// handleErr checks if an error happened and makes sure we will retry later.
func (cc *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		cc.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if cc.queue.NumRequeues(key) < 5 {
		glog.V(0).Infof("Error syncing cluster %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		cc.queue.AddRateLimited(key)
		return
	}

	cc.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	glog.V(0).Infof("Dropping cluster %q out of the queue: %v", key, err)
}

func (cc *Controller) syncInPhase(phase kubermaticv1.ClusterPhase) {
	clusters, err := cc.clusterLister.List(labels.Everything())
	if err != nil {
		cc.metrics.Clusters.Set(0)
		runtime.HandleError(fmt.Errorf("error listing clusters during phase sync %s: %v", phase, err))
		return
	}
	cc.metrics.Clusters.Set(float64(len(clusters)))

	for _, c := range clusters {
		if c.Status.Phase == phase {
			cc.queue.Add(c.Name)
		}
	}
}

// Run starts the controller's worker routines. This method is blocking and ends when stopCh gets closed
func (cc *Controller) Run(workerCount int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()

	glog.Infof("Starting cluster controller with %d workers", workerCount)
	defer glog.Info("Shutting down cluster controller")

	if !cache.WaitForCacheSync(stopCh,
		cc.clusterSynced,
		cc.etcdClusterSynced,
		cc.namespaceSynced,
		cc.secretSynced,
		cc.serviceSynced,
		cc.pvcSynced,
		cc.configMapSynced,
		cc.serviceAccountSynced,
		cc.deploymentSynced,
		cc.statefulSynced,
		cc.ingressSynced,
		cc.roleSynced,
		cc.roleBindingSynced,
		cc.clusterRoleBindingSynced) {
		runtime.HandleError(errors.New("Unable to sync caches for cluster controller"))
		return
	}

	for i := 0; i < workerCount; i++ {
		go wait.Until(cc.runWorker, time.Second, stopCh)
	}
	cc.metrics.Workers.Set(float64(workerCount))

	go wait.Until(func() { cc.syncInPhase(kubermaticv1.ValidatingClusterStatusPhase) }, validatingSyncPeriod, stopCh)
	go wait.Until(func() { cc.syncInPhase(kubermaticv1.LaunchingClusterStatusPhase) }, launchingSyncPeriod, stopCh)
	go wait.Until(func() { cc.syncInPhase(kubermaticv1.DeletingClusterStatusPhase) }, deletingSyncPeriod, stopCh)
	go wait.Until(func() { cc.syncInPhase(kubermaticv1.RunningClusterStatusPhase) }, runningSyncPeriod, stopCh)

	<-stopCh
}

func (cc *Controller) handleChildObject(i interface{}) {
	obj, ok := i.(metav1.Object)
	//Object might be a tombstone
	if !ok {
		tombstone, ok := i.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("couldn't get obj from tombstone %#v", obj))
			return
		}
		obj = tombstone.Obj.(metav1.Object)
	}

	// If it has a ControllerRef, that's all that matters.
	if controllerRef := metav1.GetControllerOf(obj); controllerRef != nil {
		if controllerRef.APIVersion != kubermaticv1.SchemeGroupVersion.String() || controllerRef.Kind != "Cluster" {
			//Not for us
			return
		}
		c, err := cc.clusterLister.Get(controllerRef.Name)
		if err != nil {
			if kubeapierrors.IsNotFound(err) {
				runtime.HandleError(fmt.Errorf("orphaned child obj found '%s/%s'. Responsible controller %s not found", obj.GetNamespace(), obj.GetName(), controllerRef.Name))
				return
			}
			runtime.HandleError(fmt.Errorf("failed to get cluster %s from lister: %v", controllerRef.Name, err))
			return
		}

		cc.enqueue(c)
		return
	}
}

func (cc *Controller) getOwnerRefForCluster(c *kubermaticv1.Cluster) metav1.OwnerReference {
	gv := kubermaticv1.SchemeGroupVersion
	return *metav1.NewControllerRef(c, gv.WithKind("Cluster"))
}
