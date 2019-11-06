/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer, via loadBalancerConfig.
type LoadBalancerController struct {
	ctx *context.ControllerContext

	nodeLister cache.Indexer
	nodes      *NodeController

	// TODO: Watch secrets
	ingQueue   utils.TaskQueue
	Translator *translator.Translator
	stopCh     chan struct{}
	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	// tlsLoader loads secrets from the Kubernetes apiserver for Ingresses.
	tlsLoader tls.TlsLoader
	// hasSynced returns true if all associated sub-controllers have synced.
	// Abstracted into a func for testing.
	hasSynced func() bool

	// Resource pools.
	instancePool instances.NodePool
	l7Pool       loadbalancers.LoadBalancerPool

	// syncer implementation for backends
	backendSyncer backends.Syncer
	// linker implementations for backends
	negLinker backends.Linker
	igLinker  backends.Linker
}

// NewLoadBalancerController creates a controller for gce loadbalancers.
func NewLoadBalancerController(
	ctx *context.ControllerContext,
	stopCh chan struct{}) *LoadBalancerController {

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.CoreV1().Events(""),
	})

	healthChecker := healthchecks.NewHealthChecker(ctx.Cloud, ctx.HealthCheckPath, ctx.DefaultBackendHealthCheckPath, ctx.DefaultBackendSvcPort.ID.Service)
	instancePool := instances.NewNodePool(ctx.Cloud, ctx.ClusterNamer)
	backendPool := backends.NewPool(ctx.Cloud, ctx.ClusterNamer)

	lbc := LoadBalancerController{
		ctx:           ctx,
		nodeLister:    ctx.NodeInformer.GetIndexer(),
		Translator:    translator.NewTranslator(ctx),
		tlsLoader:     &tls.TLSCertsFromSecretsLoader{Client: ctx.KubeClient},
		stopCh:        stopCh,
		hasSynced:     ctx.HasSynced,
		nodes:         NewNodeController(ctx, instancePool),
		instancePool:  instancePool,
		l7Pool:        loadbalancers.NewLoadBalancerPool(ctx.Cloud, ctx.ClusterNamer, ctx, namer.NewFrontendNamerFactory(ctx.ClusterNamer)),
		backendSyncer: backends.NewBackendSyncer(backendPool, healthChecker, ctx.Cloud),
		negLinker:     backends.NewNEGLinker(backendPool, negtypes.NewAdapter(ctx.Cloud), ctx.Cloud),
		igLinker:      backends.NewInstanceGroupLinker(instancePool, backendPool),
	}

	lbc.ingQueue = utils.NewPeriodicTaskQueue("ingress", "ingresses", lbc.sync)

	// Ingress event handlers.
	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*v1beta1.Ingress)
			if !utils.IsGLBCIngress(addIng) {
				klog.V(4).Infof("Ignoring add for ingress %v based on annotation %v", common.NamespacedName(addIng), annotations.IngressClassKey)
				return
			}

			klog.V(3).Infof("Ingress %v added, enqueuing", common.NamespacedName(addIng))
			lbc.ctx.Recorder(addIng.Namespace).Eventf(addIng, apiv1.EventTypeNormal, "ADD", common.NamespacedName(addIng))
			lbc.ingQueue.Enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*v1beta1.Ingress)
			if delIng == nil {
				klog.Errorf("Invalid object type: %T", obj)
				return
			}
			if delIng.ObjectMeta.DeletionTimestamp != nil {
				klog.V(2).Infof("Ignoring delete event for Ingress %v, deletion will be handled via the finalizer", common.NamespacedName(delIng))
				return
			}

			if !utils.IsGLBCIngress(delIng) {
				klog.V(4).Infof("Ignoring delete for ingress %v based on annotation %v", common.NamespacedName(delIng), annotations.IngressClassKey)
				return
			}

			klog.V(3).Infof("Ingress %v deleted, enqueueing", common.NamespacedName(delIng))
			lbc.ingQueue.Enqueue(obj)
		},
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*v1beta1.Ingress)
			if !utils.IsGLBCIngress(curIng) {
				oldIng := old.(*v1beta1.Ingress)
				// If ingress was GLBC Ingress, we need to track ingress class change
				// and run GC to delete LB resources.
				if utils.IsGLBCIngress(oldIng) {
					klog.V(4).Infof("Ingress %v class was changed, enqueuing", common.NamespacedName(curIng))
					lbc.ingQueue.Enqueue(cur)
					return
				}
				return
			}
			if reflect.DeepEqual(old, cur) {
				klog.V(3).Infof("Periodic enqueueing of %v", common.NamespacedName(curIng))
			} else {
				klog.V(3).Infof("Ingress %v changed, enqueuing", common.NamespacedName(curIng))
			}

			lbc.ingQueue.Enqueue(cur)
		},
	})

	// Service event handlers.
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*apiv1.Service)
			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				svc := cur.(*apiv1.Service)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			}
		},
		// Ingress deletes matter, service deletes don't.
	})

	// BackendConfig event handlers.
	ctx.BackendConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			beConfig := obj.(*backendconfigv1beta1.BackendConfig)
			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List())).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				beConfig := cur.(*backendconfigv1beta1.BackendConfig)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List())).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var beConfig *backendconfigv1beta1.BackendConfig
			var ok, beOk bool
			beConfig, ok = obj.(*backendconfigv1beta1.BackendConfig)
			if !ok {
				// This can happen if the watch is closed and misses the delete event
				state, stateOk := obj.(cache.DeletedFinalStateUnknown)
				if !stateOk {
					klog.Errorf("Wanted cache.DeleteFinalStateUnknown of backendconfig obj, got: %+v", obj)
					return
				}

				beConfig, beOk = state.Obj.(*backendconfigv1beta1.BackendConfig)
				if !beOk {
					klog.Errorf("Wanted backendconfig obj, got %+v", state.Obj)
					return
				}
			}

			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesBackendConfig(beConfig, operator.Services(ctx.Services().List())).AsList()
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
	})

	// FrontendConfig event handlers.
	if ctx.FrontendConfigEnabled {
		ctx.FrontendConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				feConfig := obj.(*frontendconfigv1beta1.FrontendConfig)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)

			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					feConfig := cur.(*frontendconfigv1beta1.FrontendConfig)
					ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
					lbc.ingQueue.Enqueue(convert(ings)...)
				}
			},
			DeleteFunc: func(obj interface{}) {
				var feConfig *frontendconfigv1beta1.FrontendConfig
				var ok, feOk bool
				feConfig, ok = obj.(*frontendconfigv1beta1.FrontendConfig)
				if !ok {
					// This can happen if the watch is closed and misses the delete event
					state, stateOk := obj.(cache.DeletedFinalStateUnknown)
					if !stateOk {
						klog.Errorf("Wanted cache.DeleteFinalStateUnknown of frontendconfig obj, got: %+v type: %T", obj, obj)
						return
					}

					feConfig, feOk = state.Obj.(*frontendconfigv1beta1.FrontendConfig)
					if !feOk {
						klog.Errorf("Wanted frontendconfig obj, got %+v, type %T", state.Obj, state.Obj)
						return
					}
				}

				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesFrontendConfig(feConfig).AsList()
				lbc.ingQueue.Enqueue(convert(ings)...)
			},
		})
	}

	// Register health check on controller context.
	ctx.AddHealthCheck("ingress", func() error {
		_, err := backendPool.Get("foo", meta.VersionGA, meta.Global)

		// If this container is scheduled on a node without compute/rw it is
		// effectively useless, but it is healthy. Reporting it as unhealthy
		// will lead to container crashlooping.
		if utils.IsHTTPErrorCode(err, http.StatusForbidden) {
			klog.Infof("Reporting cluster as healthy, but unable to list backends: %v", err)
			return nil
		}
		return utils.IgnoreHTTPNotFound(err)
	})

	klog.V(3).Infof("Created new loadbalancer controller")

	return &lbc
}

func (lbc *LoadBalancerController) Init() {
	// TODO(rramkumar): Try to get rid of this "Init".
	lbc.instancePool.Init(lbc.Translator)
	lbc.backendSyncer.Init(lbc.Translator)
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	klog.Infof("Starting loadbalancer controller")
	go lbc.ingQueue.Run()
	go lbc.nodes.Run()

	<-lbc.stopCh
	klog.Infof("Shutting down Loadbalancer Controller")
}

// Stop stops the loadbalancer controller. It also deletes cluster resources
// if deleteAll is true.
func (lbc *LoadBalancerController) Stop(deleteAll bool) error {
	// Stop is invoked from the http endpoint.
	lbc.stopLock.Lock()
	defer lbc.stopLock.Unlock()

	// Only try draining the workqueue if we haven't already.
	if !lbc.shutdown {
		close(lbc.stopCh)
		klog.Infof("Shutting down controller queues.")
		lbc.ingQueue.Shutdown()
		lbc.nodes.Shutdown()
		lbc.shutdown = true
	}

	// Deleting shared cluster resources is idempotent.
	// TODO(rramkumar): Do we need deleteAll? Can we get rid of its' flag?
	if deleteAll {
		klog.Infof("Shutting down cluster manager.")
		if err := lbc.l7Pool.Shutdown(); err != nil {
			return err
		}
		// The backend pool will also delete instance groups.
		return lbc.backendSyncer.Shutdown()
	}
	return nil
}

// SyncBackends syncs the backends for a GCLB given some existing state.
func (lbc *LoadBalancerController) SyncBackends(state *syncState) error {
	ingSvcPorts := state.urlMap.AllServicePorts()

	// Create instance groups and set named ports.
	igs, err := lbc.instancePool.EnsureInstanceGroupsAndPorts(lbc.ctx.ClusterNamer.InstanceGroup(), nodePorts(ingSvcPorts))
	if err != nil {
		return err
	}

	nodeNames, err := utils.GetReadyNodeNames(listers.NewNodeLister(lbc.nodeLister))
	if err != nil {
		return err
	}
	// Add/remove instances to the instance groups.
	if err = lbc.instancePool.Sync(nodeNames); err != nil {
		return err
	}

	// TODO: Remove this after deprecation
	ing := state.ing
	if utils.IsGCEMultiClusterIngress(state.ing) {
		// Add instance group names as annotation on the ingress and return.
		if ing.Annotations == nil {
			ing.Annotations = map[string]string{}
		}
		if err = setInstanceGroupsAnnotation(ing.Annotations, igs); err != nil {
			return err
		}
		if err = updateAnnotations(lbc.ctx.KubeClient, ing.Name, ing.Namespace, ing.Annotations); err != nil {
			return err
		}
		// This short-circuit will stop the syncer from moving to next step.
		return ErrSkipBackendsSync
	}

	// Sync the backends
	if err := lbc.backendSyncer.Sync(ingSvcPorts); err != nil {
		return err
	}

	// Get the zones our groups live in.
	zones, err := lbc.Translator.ListZones()
	var groupKeys []backends.GroupKey
	for _, zone := range zones {
		groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
	}

	// Link backends to groups.
	for _, sp := range ingSvcPorts {
		var linkErr error
		if sp.NEGEnabled {
			// Link backend to NEG's if the backend has NEG enabled.
			linkErr = lbc.negLinker.Link(sp, groupKeys)
		} else {
			// Otherwise, link backend to IG's.
			linkErr = lbc.igLinker.Link(sp, groupKeys)
		}
		if linkErr != nil {
			return linkErr
		}
	}

	return nil
}

// GCBackends garbage collects backends for all ingresses given a list of ingresses to exclude from GC.
func (lbc *LoadBalancerController) GCBackends(state *gcState) error {
	svcPortsToKeep := lbc.ToSvcPorts(state.toKeep)
	if err := lbc.backendSyncer.GC(svcPortsToKeep); err != nil {
		return err
	}
	// TODO(ingress#120): Move this to the backend pool so it mirrors creation
	// Do not delete instance group if there exists a GLBC ingress.
	if state.toKeepCount == 0 {
		igName := lbc.ctx.ClusterNamer.InstanceGroup()
		klog.Infof("Deleting instance group %v", igName)
		if err := lbc.instancePool.DeleteInstanceGroup(igName); err != err {
			return err
		}
	}
	return nil
}

// SyncLoadBalancer syncs the front-end load balancer resources for a GCLB given some existing state.
func (lbc *LoadBalancerController) SyncLoadBalancer(state *syncState) error {
	lb, err := lbc.toRuntimeInfo(state.ing, state.urlMap)
	if err != nil {
		return err
	}

	// Create higher-level LB resources.
	l7, err := lbc.l7Pool.Ensure(lb)
	if err != nil {
		return err
	}

	state.l7 = l7
	return nil
}

// GCLoadBalancers garbage collects front-end load balancer resources for
// all ingresses given a list of ingresses to exclude from GC.
func (lbc *LoadBalancerController) GCLoadBalancers(state *gcState) error {
	// Only GCE ingress associated resources are managed by this controller.
	GCEIngresses := operator.Ingresses(state.toKeep).Filter(utils.IsGCEIngress).AsList()
	return lbc.l7Pool.GC(common.ToIngressKeys(GCEIngresses))
}

// EnsureDeleteFinalizers ensures that finalizers are removed.
func (lbc *LoadBalancerController) EnsureDeleteFinalizers(state *gcState) error {
	if !flags.F.FinalizerRemove {
		klog.V(4).Infof("Removing finalizers not enabled")
		return nil
	}
	for _, ing := range state.toCleanup {
		ingClient := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace)
		if err := common.RemoveFinalizer(ing, ingClient); err != nil {
			klog.Errorf("Failed to remove Finalizer from Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
			return err
		}
	}
	return nil
}

// PostProcess allows for doing some post-processing after an Ingress is synced to a GCLB.
func (lbc *LoadBalancerController) PostProcess(state *syncState) error {
	// Update the ingress status.
	return lbc.updateIngressStatus(state.l7, state.ing)
}

// sync manages Ingress create/updates/deletes events from queue.
func (lbc *LoadBalancerController) sync(key string) error {
	if !lbc.hasSynced() {
		time.Sleep(context.StoreSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	klog.V(3).Infof("Syncing %v", key)

	ing, ingExists, err := lbc.ctx.Ingresses().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error getting Ingress for key %s: %v", key, err)
	}

	// Snapshot of list of all ingresses.
	gcState := newGcState(ing, lbc.ctx.Ingresses().List())
	// Determine if the ingress needs to be GCed.
	if !ingExists || utils.NeedsCleanup(ing) {
		// GC will find GCE resources that were used for this ingress and delete them.
		return lbc.GC(gcState)
	}

	// Get ingress and DeepCopy for assurance that we don't pollute other goroutines with changes.
	ing = ing.DeepCopy()
	ingClient := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace)
	if flags.F.FinalizerAdd {
		if err := common.AddFinalizer(ing, ingClient); err != nil {
			klog.Errorf("Failed to add Finalizer to Ingress %q: %v", key, err)
			return err
		}
	}

	// Bootstrap state for GCP sync.
	urlMap, errs := lbc.Translator.TranslateIngress(ing, lbc.ctx.DefaultBackendSvcPort.ID, lbc.ctx.ClusterNamer)

	if errs != nil {
		msg := fmt.Errorf("error while evaluating the ingress spec: %v", utils.JoinErrs(errs))
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Translate", msg.Error())
		return msg
	}

	// Sync GCP resources.
	syncState := &syncState{urlMap, ing, nil}
	syncErr := lbc.Sync(syncState)
	if syncErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Sync", fmt.Sprintf("Error during sync: %v", syncErr.Error()))
	}

	// Garbage collection will occur regardless of an error occurring. If an error occurred,
	// it could have been caused by quota issues; therefore, garbage collecting now may
	// free up enough quota for the next sync to pass.
	if gcErr := lbc.GC(gcState); gcErr != nil {
		return fmt.Errorf("error during sync %v, error during GC %v", syncErr, gcErr)
	}

	return syncErr
}

// updateIngressStatus updates the IP and annotations of a loadbalancer.
// The annotations are parsed by kubectl describe.
func (lbc *LoadBalancerController) updateIngressStatus(l7 *loadbalancers.L7, ing *v1beta1.Ingress) error {
	ingClient := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace)

	// Update IP through update/status endpoint
	ip := l7.GetIP()
	currIng, err := ingClient.Get(ing.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	currIng.Status = v1beta1.IngressStatus{
		LoadBalancer: apiv1.LoadBalancerStatus{
			Ingress: []apiv1.LoadBalancerIngress{
				{IP: ip},
			},
		},
	}
	if ip != "" {
		lbIPs := ing.Status.LoadBalancer.Ingress
		if len(lbIPs) == 0 || lbIPs[0].IP != ip {
			// TODO: If this update fails it's probably resource version related,
			// which means it's advantageous to retry right away vs requeuing.
			klog.Infof("Updating loadbalancer %v/%v with IP %v", ing.Namespace, ing.Name, ip)
			if _, err := ingClient.UpdateStatus(currIng); err != nil {
				return err
			}
			lbc.ctx.Recorder(ing.Namespace).Eventf(currIng, apiv1.EventTypeNormal, "CREATE", "ip: %v", ip)
		}
	}
	annotations, err := loadbalancers.GetLBAnnotations(l7, currIng.Annotations, lbc.backendSyncer)
	if err != nil {
		return err
	}

	if err := updateAnnotations(lbc.ctx.KubeClient, ing.Name, ing.Namespace, annotations); err != nil {
		return err
	}
	return nil
}

// toRuntimeInfo returns L7RuntimeInfo for the given ingress.
func (lbc *LoadBalancerController) toRuntimeInfo(ing *v1beta1.Ingress, urlMap *utils.GCEURLMap) (*loadbalancers.L7RuntimeInfo, error) {
	annotations := annotations.FromIngress(ing)
	tls, err := lbc.tlsLoader.Load(ing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// TODO: this path should be removed when external certificate managers migrate to a better solution.
			const msg = "Could not find TLS certificates. Continuing setup for the load balancer to serve HTTP. Note: this behavior is deprecated and will be removed in a future version of ingress-gce"
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Sync", msg)
		} else {
			klog.Errorf("Could not get certificates for ingress %s/%s: %v", ing.Namespace, ing.Name, err)
			return nil, err
		}
	}

	var feConfig *frontendconfigv1beta1.FrontendConfig
	if lbc.ctx.FrontendConfigEnabled {
		feConfig, err = frontendconfig.FrontendConfigForIngress(lbc.ctx.FrontendConfigs().List(), ing)
		if err != nil {
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Sync", fmt.Sprintf("%v", err))
		}
		// Object in cache could be changed in-flight. Deepcopy to
		// reduce race conditions.
		feConfig = feConfig.DeepCopy()
	}

	return &loadbalancers.L7RuntimeInfo{
		TLS:            tls,
		TLSName:        annotations.UseNamedTLS(),
		Ingress:        ing,
		AllowHTTP:      annotations.AllowHTTP(),
		StaticIPName:   annotations.StaticIPName(),
		UrlMap:         urlMap,
		FrontendConfig: feConfig,
	}, nil
}

func updateAnnotations(client kubernetes.Interface, name, namespace string, annotations map[string]string) error {
	ingClient := client.NetworkingV1beta1().Ingresses(namespace)
	currIng, err := ingClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(currIng.Annotations, annotations) {
		klog.V(3).Infof("Updating annotations of %v/%v", namespace, name)
		currIng.Annotations = annotations
		if _, err := ingClient.Update(currIng); err != nil {
			return err
		}
	}
	return nil
}

// ToSvcPorts returns a list of SVC ports given a list of ingresses.
// Note: This method is used for GC.
func (lbc *LoadBalancerController) ToSvcPorts(ings []*v1beta1.Ingress) []utils.ServicePort {
	var knownPorts []utils.ServicePort
	for _, ing := range ings {
		urlMap, _ := lbc.Translator.TranslateIngress(ing, lbc.ctx.DefaultBackendSvcPort.ID, lbc.ctx.ClusterNamer)
		knownPorts = append(knownPorts, urlMap.AllServicePorts()...)
	}
	return knownPorts
}
