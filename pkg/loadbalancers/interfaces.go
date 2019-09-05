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

package loadbalancers

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// LoadBalancerPool is an interface to manage the cloud resources associated
// with a gce loadbalancer.
type LoadBalancerPool interface {
	Ensure(ri *L7RuntimeInfo) (*L7, error)
	Delete(ing *v1beta1.Ingress, versions *features.ResourceVersions, scope meta.KeyType) error
	GC(ing []*v1beta1.Ingress) error
	Shutdown() error
	List(key *meta.Key, version meta.Version) ([]*composite.UrlMap, error)
	GetFrontendNamer(ing *v1beta1.Ingress) namer.IngressFrontendNamer
}
