/*
Copyright 2017 The Kubernetes Authors.

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

package app

import (
	"crypto/rand"
	"fmt"
	"time"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	// Key used to persist UIDs to configmaps.
	uidConfigMapName = "ingress-uid"
	// uidByteLength is the length in bytes for the random UID.
	uidByteLength = 8
)

// NewNamer returns a new naming policy given the state of the cluster.
func NewNamer(kubeClient kubernetes.Interface, clusterName, fwName string) (*namer_util.Namer, error) {
	namer, useCfgMap, err := NewStaticNamer(kubeClient, clusterName, fwName)
	if err != nil {
		return nil, err
	}

	// Cluster UID and firewall name are dynamically updated only when we are not
	// using kube system UID.
	if useCfgMap {
		uidVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName)

		// Start a goroutine to poll the cluster UID config map.  We don't
		// watch because we know exactly which configmap we want and this
		// controller already watches 5 other resources, so it isn't worth the
		// cost of another connection and complexity.
		go wait.Forever(func() {
			for _, key := range [...]string{storage.UIDDataKey, storage.ProviderDataKey} {
				val, found, err := uidVault.Get(key)
				if err != nil {
					klog.Errorf("Can't read uidConfigMap %v", uidConfigMapName)
				} else if !found {
					errmsg := fmt.Sprintf("Can't read %v from uidConfigMap %v", key, uidConfigMapName)
					if key == storage.UIDDataKey {
						klog.Errorf(errmsg)
					} else {
						klog.V(4).Infof(errmsg)
					}
				} else {

					switch key {
					case storage.UIDDataKey:
						if uid := namer.UID(); uid != val {
							klog.Infof("Cluster uid changed from %v -> %v", uid, val)
							namer.SetUID(val)
						}
					case storage.ProviderDataKey:
						if fw_name := namer.Firewall(); fw_name != val {
							klog.Infof("Cluster firewall name changed from %v -> %v", fw_name, val)
							namer.SetFirewall(val)
						}
					}
				}
			}
		}, 5*time.Second)
	}
	return namer, nil
}

// NewStaticNamer returns a new naming policy given a snapshot of cluster state. Note that this
// implementation does not dynamically change the naming policy based on changes in cluster state.
// This also returns a boolean flag to inform if we are using config map vault to
// retrieve cluster UID and firewall name.
func NewStaticNamer(kubeClient kubernetes.Interface, clusterName, fwName string) (*namer_util.Namer, bool, error) {
	cfgVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName)
	name, useCfgMap, err := getClusterUID(kubeClient, cfgVault, clusterName)
	if err != nil {
		return nil, useCfgMap, err
	}
	fw_name, useCfgMap, err := getFirewallName(cfgVault, fwName, name, useCfgMap)
	if err != nil {
		return nil, useCfgMap, err
	}

	return namer_util.NewNamer(name, fw_name), useCfgMap, nil
}

// useDefaultOrLookupVault returns either a 'defaultName' or if unset, obtains
// a name from a ConfigMap.  The returned value follows this priority:
//
// If the provided 'defaultName' is not empty, that name is used.
//     This is effectively a client override via a command line flag.
// else, check cfgVault with 'configMapKey' as a key and if found, use the associated value
// else, return an empty 'name' and pass along an error iff the configmap lookup is erroneous.
// This also returns a boolean flag to inform if we are using config map vault to
// retrieve given key.
func useDefaultOrLookupVault(cfgVault *storage.ConfigMapVault, configMapKey, defaultName string) (string, bool, error) {
	if defaultName != "" {
		klog.Infof("Using user provided %v %v", configMapKey, defaultName)
		// Don't save the uid in the vault, so users can rollback
		// through setting the accompany flag to ""
		return defaultName, false, nil
	}
	val, found, err := cfgVault.Get(configMapKey)
	if err != nil {
		// This can fail because of:
		// 1. No such config map - found=false, err=nil
		// 2. No such key in config map - found=false, err=nil
		// 3. Apiserver flake - found=false, err!=nil
		// It is not safe to proceed in 3.
		return "", false, fmt.Errorf("failed to retrieve %v: %v, returning empty name", configMapKey, err)
	} else if !found {
		// Not found but safe to proceed.
		return "", false, nil
	}
	klog.Infof("Using %v = %q saved in ConfigMap", configMapKey, val)
	return val, true, nil
}

// getFirewallName returns the firewall rule name to use for this cluster. For
// backwards compatibility, the firewall name will default to the cluster UID.
// Use getDefaultOrLookupVault to obtain a stored or overridden value for the firewall name.
// else, use the cluster UID as a backup (this retains backwards compatibility).
// This also returns a boolean flag to inform if we are using config map vault to
// retrieve cluster UID and firewall name.
func getFirewallName(cfgVault *storage.ConfigMapVault, name, clusterUID string, useCfgMapForUID bool) (string, bool, error) {
	firewallName, useCfgMap, err := useDefaultOrLookupVault(cfgVault, storage.ProviderDataKey, name)
	if err != nil {
		return "", useCfgMap, err
	} else if firewallName != "" {
		if useCfgMap || useCfgMapForUID {
			return firewallName, true, cfgVault.Put(storage.ProviderDataKey, firewallName)
		}
		return firewallName, false, nil
	} else {
		klog.Infof("Using cluster UID %v as firewall name", clusterUID)
		if useCfgMapForUID {
			return firewallName, true, cfgVault.Put(storage.ProviderDataKey, clusterUID)
		}
		return clusterUID, false, nil
	}
}

// getClusterUID returns the cluster UID. Rules for UID generation:
// If the user specifies a --cluster-uid param it overwrites everything
// else, check UID config map for a previously recorded uid
// else, check if there are any working Ingresses
//  - If there exists a v1 ingress, we return cluster UID parsing a resource name
//  associated with that ingress.
//  - else if, there exist a v2 ingress(with flags.F.FinalizerAdd enabled),
//  we return cluster UID parsing a resource name associated with it.
//	- remember that "" is the cluster uid
// else if, flags.F.FinalizerAdd is enabled, use kube-system uid
// else, allocate a new uid
// This also returns a boolean flag to inform if we are using config map vault to
// retrieve cluster UID.
func getClusterUID(kubeClient kubernetes.Interface, cfgVault *storage.ConfigMapVault, name string) (string, bool, error) {
	name, useCfgMap, err := useDefaultOrLookupVault(cfgVault, storage.UIDDataKey, name)
	if err != nil {
		return "", useCfgMap, err
	} else if name != "" {
		return name, useCfgMap, nil
	}

	// Check if the cluster has an Ingress with ip
	ings, err := kubeClient.NetworkingV1beta1().Ingresses(metav1.NamespaceAll).List(metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	})
	if err != nil {
		return "", false, err
	}
	namer := namer_util.NewNamer("", "")
	// The following logic parses the cluster id from an working ingress. if there is
	// at least one working ingress with v1 naming scheme we would use that to parse
	// cluster uid. Otherwise we use v2 ingress.
	// v2Ing is an ingress that uses v2 naming scheme.
	var v2Ing *v1beta1.Ingress
	for _, ing := range ings.Items {
		if flags.F.FinalizerAdd && utils.HasGivenFinalizer(ing.ObjectMeta, utils.FinalizerKeyV2) {
			if v2Ing == nil && len(ing.Status.LoadBalancer.Ingress) != 0 {
				v2Ing = &ing
			}
			continue
		}
		if len(ing.Status.LoadBalancer.Ingress) != 0 {
			c := namer.ParseName(loadbalancers.GCEResourceName(ing.Annotations, "forwarding-rule"))
			if c.ClusterName != "" {
				return c.ClusterName, true, nil
			}
			klog.Infof("Found a working Ingress, assuming uid is empty string")
			return "", true, nil
		}
	}

	if flags.F.FinalizerAdd {
		var uid string
		if v2Ing != nil {
			uid = namer_util.ParseV2ClusterUID(loadbalancers.GCEResourceName(v2Ing.Annotations, "url-map"))
		} else {
			// Use kube-system uid as cluster uid.
			ksNameSpace, err := kubeClient.CoreV1().Namespaces().Get("kube-system", metav1.GetOptions{})
			if err != nil {
				return "", false, fmt.Errorf("error getting kube-system uid: %v", err)
			}
			uid = namer_util.Hash(string(ksNameSpace.GetUID()), namer_util.ClusterUIDLength)
		}
		return uid, false, nil
	}

	uid, err := randomUID()
	if err != nil {
		return "", false, err
	}
	return uid, true, cfgVault.Put(storage.UIDDataKey, uid)
}

func randomUID() (string, error) {
	b := make([]byte, uidByteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	uid := fmt.Sprintf("%x", b)
	return uid, nil
}
