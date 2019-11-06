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

package controller

import (
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
)

// syncState is used by the controller to maintain state for routines that sync GCP resources of an Ingress.
type syncState struct {
	// urlMap is the desired state of URL map after the sync.
	urlMap *utils.GCEURLMap
	// ing is the ingress being synced.
	ing *v1beta1.Ingress
	// l7 is the loadbalancer associated with the ingress being synced.
	l7 *loadbalancers.L7
}

// gcState represents snapshot of all ingresses in the ingress store at the start of sync routine.
// This is used by the controller for gc'ing ingresses.
type gcState struct {
	// currIng is the ingress being synced.
	currIng *v1beta1.Ingress
	// allIngs is the list of all ingresses in the ingress store.
	allIngs []*v1beta1.Ingress
	// toKeepCount is the total number of ingresses that will continue to live after the sync.
	// This includes multi-cluster ingresses as well. This value is used to determine if
	// we need to delete the instance group.
	toKeepCount int
	// toKeep is the list of GCE ingresses that needs to exist after the sync.
	toKeep []*v1beta1.Ingress
	// toCleanup is the list of ingresses that needs to deleted during the sync.
	toCleanup []*v1beta1.Ingress
}

func newGcState(currIng *v1beta1.Ingress, allIngs []*v1beta1.Ingress) *gcState {
	// Partition GC state into ingresses those need cleanup and those don't.
	// An Ingress is considered to exist and not considered for cleanup, if:
	// 1) It is a GCE Ingress.
	// 2) It is not a candidate for deletion.
	toCleanup, toKeepGCLB := operator.Ingresses(allIngs).Partition(utils.NeedsCleanup)
	toKeepGCE := toKeepGCLB.Filter(utils.IsGCEIngress)
	state := &gcState{currIng: currIng, allIngs: allIngs}
	state.toCleanup = toCleanup.AsList()
	state.toKeep = toKeepGCE.AsList()
	state.toKeepCount = len(toKeepGCLB.AsList())
	return state
}
