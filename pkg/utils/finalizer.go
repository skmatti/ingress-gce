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

package utils

import (
	"fmt"

	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes/typed/networking/v1beta1"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/slice"
)

// FinalizerKey is the string representing the Ingress finalizer.
const (
	FinalizerKey   = "networking.gke.io/ingress-finalizer"
	FinalizerKeyV2 = "networking.gke.io/ingress-finalizer-V2"
)

// IsDeletionCandidate is true if the passed in meta contains the specified finalizer.
func IsDeletionCandidate(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp != nil && HasGivenFinalizer(m, key)
}

// NeedToAddFinalizer is true if the passed in meta does not contain the specified finalizer.
func NeedToAddFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp == nil && !HasGivenFinalizer(m, key)
}

// HasFinalizer is true if the passed in meta has one or more of the finalizers.
func HasFinalizer(m meta_v1.ObjectMeta) bool {
	return slice.ContainsString(m.Finalizers, FinalizerKey, nil) || slice.ContainsString(m.Finalizers, FinalizerKeyV2, nil)
}

// HasGivenFinalizer is true if the passed in meta has the specified finalizer.
func HasGivenFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return slice.ContainsString(m.Finalizers, key, nil)
}

// AddFinalizer tries to add a finalizer to an Ingress. If a finalizer
// already exists, it does nothing.
func AddFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface) error {
	return AddGivenFinalizer(ing, ingClient, FinalizerKey)
}

// AddFinalizer tries to add given finalizer key to an Ingress. If a finalizer
// already exists, it does nothing.
func AddGivenFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface, finalizerKey string) error {
	if NeedToAddFinalizer(ing.ObjectMeta, finalizerKey) {
		updated := ing.DeepCopy()
		updated.ObjectMeta.Finalizers = append(updated.ObjectMeta.Finalizers, finalizerKey)
		if _, err := ingClient.Update(updated); err != nil {
			return fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(3).Infof("Added finalizer %q for Ingress %s/%s", finalizerKey, ing.Namespace, ing.Name)
	}

	return nil
}

// RemoveFinalizer tries to remove a Finalizer from an Ingress. If a
// finalizer is not on the Ingress, it does nothing.
func RemoveFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface) error {
	if err := RemoveGivenFinalizer(ing, ingClient, FinalizerKey); err != nil {
		return err
	}
	return RemoveGivenFinalizer(ing, ingClient, FinalizerKeyV2)
}

// RemoveFinalizer tries to remove given Finalizer from an Ingress. If a
// finalizer is not on the Ingress, it does nothing.
func RemoveGivenFinalizer(ing *v1beta1.Ingress, ingClient client.IngressInterface, finalizerKey string) error {
	if HasGivenFinalizer(ing.ObjectMeta, finalizerKey) {
		updated := ing.DeepCopy()
		updated.ObjectMeta.Finalizers = slice.RemoveString(updated.ObjectMeta.Finalizers, finalizerKey, nil)
		if _, err := ingClient.Update(updated); err != nil {
			return fmt.Errorf("error updating Ingress %s/%s: %v", ing.Namespace, ing.Name, err)
		}
		klog.V(3).Infof("Removed finalizer %q for Ingress %s/%s", finalizerKey, ing.Namespace, ing.Name)
	}

	return nil
}
