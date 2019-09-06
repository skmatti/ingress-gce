/*
Copyright 2018 The Kubernetes Authors.

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

package loadbalancers

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// L7s implements LoadBalancerPool.
type L7s struct {
	cloud            *gce.Cloud
	namer            *namer.Namer
	recorderProducer events.RecorderProducer
}

// Namer returns the feNamer associated with the L7s.
func (l *L7s) Namer() *namer.Namer {
	return l.namer
}

// NewLoadBalancerPool returns a new loadbalancer pool.
// - cloud: implements LoadBalancers. Used to sync L7 loadbalancer resources
//	 with the cloud.
func NewLoadBalancerPool(cloud *gce.Cloud, namer *namer.Namer, recorderProducer events.RecorderProducer) LoadBalancerPool {
	return &L7s{
		cloud:            cloud,
		namer:            namer,
		recorderProducer: recorderProducer,
	}
}

// Ensure ensures a loadbalancer and its resources given the RuntimeInfo
func (l *L7s) Ensure(ri *L7RuntimeInfo) (*L7, error) {
	lb := &L7{
		runtimeInfo: ri,
		cloud:       l.cloud,
		feNamer:     l.GetFrontendNamer(ri.Ingress),
		recorder:    l.recorderProducer.Recorder(ri.Ingress.Namespace),
		scope:       features.ScopeFromIngress(ri.Ingress),
		ingress:     *ri.Ingress,
	}

	if err := lb.edgeHop(); err != nil {
		return nil, fmt.Errorf("loadbalancer %v does not exist: %v", lb.GetName(), err)
	}
	return lb, nil
}

// Delete deletes a load balancer by name.
func (l *L7s) Delete(ing *v1beta1.Ingress, versions *features.ResourceVersions, scope meta.KeyType) error {
	feNamer := l.GetFrontendNamer(ing)
	lb := &L7{
		runtimeInfo: &L7RuntimeInfo{
			Ingress: ing,
		},
		cloud:   l.cloud,
		feNamer: feNamer,
		scope:   scope,
	}

	klog.V(3).Infof("Deleting lb %v", lb.GetName())

	if err := lb.Cleanup(versions); err != nil {
		return err
	}
	return nil
}

// List returns a list of urlMaps (the top level LB resource) that belong to the cluster
func (l *L7s) List(key *meta.Key, version meta.Version) ([]*composite.UrlMap, error) {
	var result []*composite.UrlMap
	urlMaps, err := composite.ListUrlMaps(l.cloud, key, version)
	if err != nil {
		return nil, err
	}

	for _, um := range urlMaps {
		if l.namer.NameBelongsToCluster(um.Name) || namer.IsV2UrlMap(um.Name, l.namer) {
			result = append(result, um)
		}
	}

	return result, nil
}

// GC garbage collects loadbalancers not in the input list.
// TODO(shance): Update to handle regional and global LB with same name
func (l *L7s) GC(ings []*v1beta1.Ingress) error {
	klog.V(2).Infof("GC(%v)", utils.ToLbNames(ings))

	toCleanup := make(map[string]*v1beta1.Ingress)
	for _, ing := range ings {
		toCleanup[l.GetFrontendNamer(ing).UrlMap()] = ing
	}

	// GC L7-ILB LBs if enabled
	if flags.F.EnableL7Ilb {
		key, err := composite.CreateKey(l.cloud, "", meta.Regional)
		if err != nil {
			return fmt.Errorf("error getting regional ing: %v", err)
		}
		urlMaps, err := l.List(key, features.L7ILBVersions().UrlMap)
		if err != nil {
			return fmt.Errorf("error listing regional LBs: %v", err)
		}

		if err := l.gc(urlMaps, toCleanup, features.L7ILBVersions()); err != nil {
			return fmt.Errorf("error gc-ing regional LBs: %v", err)
		}
	}

	urlMaps, err := l.List(meta.GlobalKey(""), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("error listing global LBs: %v", err)
	}

	if errors := l.gc(urlMaps, toCleanup, features.GAResourceVersions); errors != nil {
		return fmt.Errorf("error gcing global LBs: %v", errors)
	}

	return nil
}

// gc is a helper for GC
// TODO(shance): get versions from description
func (l *L7s) gc(urlMaps []*composite.UrlMap, toCleanup map[string]*v1beta1.Ingress, versions *features.ResourceVersions) []error {
	var errors []error
	// Delete unknown loadbalancers
	for _, um := range urlMaps {

		ing, ok := toCleanup[um.Name]
		if !ok {
			klog.V(3).Infof("Load balancer associated with URL Map %s is still valid, not GC'ing", um.Name)
			continue
		}
		scope, err := composite.ScopeFromSelfLink(um.SelfLink)
		if err != nil {
			errors = append(errors, fmt.Errorf("error getting scope from self link for urlMap %v: %v", um, err))
			continue
		}

		klog.V(2).Infof("GCing loadbalancer for Ingress %v", ing)
		if err := l.Delete(ing, versions, scope); err != nil {
			errors = append(errors, fmt.Errorf("error deleting loadbalancer for Ingress %v", ing))
		}
	}
	return nil
}

// Shutdown logs whether or not the pool is empty.
func (l *L7s) Shutdown() error {
	if err := l.GC([]*v1beta1.Ingress{}); err != nil {
		return err
	}
	klog.V(2).Infof("Loadbalancer pool shutdown.")
	return nil
}

// GetFrontendNamer return front based on ingress scheme
// Shutdown logs whether or not the pool is empty.
func (l *L7s) GetFrontendNamer(ing *v1beta1.Ingress) namer.IngressFrontendNamer {
	frondEndNamerFactory := namer.NewIngressFrontendNamerFactoryImpl(l.namer)
	if !flags.F.FinalizerAdd {
		return frondEndNamerFactory.CreateIngressFrontendNamer(namer.LegacyNamingScheme, ing)
	}

	if utils.HasGivenFinalizer(ing.ObjectMeta, utils.FinalizerKeyV2) {
		return frondEndNamerFactory.CreateIngressFrontendNamer(namer.V2NamingScheme, ing)
	} else if utils.HasGivenFinalizer(ing.ObjectMeta, utils.FinalizerKey) {
		return frondEndNamerFactory.CreateIngressFrontendNamer(namer.LegacyNamingScheme, ing)
	}

	klog.V(2).Infof("No naming scheme found for Ingress %v, using Legacy naming scheme", ing)
	return frondEndNamerFactory.CreateIngressFrontendNamer(namer.LegacyNamingScheme, ing)
}
