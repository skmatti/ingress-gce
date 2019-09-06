/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/flags"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"

	"k8s.io/ingress-gce/pkg/storage"
)

const (
	configMapUID         = "ConfigMapUID"
	configMapProviderUID = "ConfigMapProviderUID"
	// random is the keyword to inform test that we expect random value.
	random = "random"
)

// saveFinalizerFlags captures current value of finalizer flags and
// restore them after a test is finished.
type saveFinalizerFlags struct{ add bool }

func (s *saveFinalizerFlags) save() {
	s.add = flags.F.FinalizerAdd
}
func (s *saveFinalizerFlags) reset() {
	flags.F.FinalizerAdd = s.add
}

func TestNewStaticNamer(t *testing.T) {
	var flagSaver saveFinalizerFlags
	flagSaver.save()
	defer flagSaver.reset()

	kubeClient := fake.NewSimpleClientset()
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	ksNameSpace, err := kubeClient.CoreV1().Namespaces().Create(ns)
	if err != nil {
		t.Fatalf("Error creating kube-system namespace: %v", err)
	}
	ksNameSpace, err = kubeClient.CoreV1().Namespaces().Get("kube-system", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Error getting kube-system uid: %v", err)
	}
	kubeSystemUID := namer_util.Hash(string(ksNameSpace.GetUID()), namer_util.ClusterUIDLength)

	desc := "ClusterName:%q FirewallName:%q finalizer:%t emptyConfigMap:%t"

	testCases := []struct {
		enableFinalizer     bool
		clusterName         string
		fwName              string
		emptyConfigMap      bool
		expectedClusterName string
		expectedFwName      string
		expectedUseCfgMap   bool
	}{
		{false, "cluster1", "fw", false, "cluster1", "fw", false},
		{false, "cluster1", "fw", true, "cluster1", "fw", false},
		{false, "cluster1", "", false, "cluster1", configMapProviderUID, true},
		{false, "cluster1", "", true, "cluster1", "cluster1", false},
		{false, "", "fw", false, configMapUID, "fw", true},
		{false, "", "fw", true, random, "fw", true},
		{false, "", "", false, configMapUID, configMapProviderUID, true},
		{false, "", "", true, random, random, true},
		{true, "cluster1", "fw", false, "cluster1", "fw", false},
		{true, "cluster1", "fw", true, "cluster1", "fw", false},
		{true, "cluster1", "", false, "cluster1", configMapProviderUID, true},
		{true, "cluster1", "", true, "cluster1", "cluster1", false},
		{true, "", "fw", false, configMapUID, "fw", true},
		{true, "", "fw", true, kubeSystemUID, "fw", false},
		{true, "", "", false, configMapUID, configMapProviderUID, true},
		{true, "", "", true, kubeSystemUID, kubeSystemUID, false},
	}

	for _, tc := range testCases {
		testDesc := fmt.Sprintf(desc, tc.clusterName, tc.fwName, tc.enableFinalizer, tc.emptyConfigMap)
		t.Run(testDesc, func(t *testing.T) {
			if tc.enableFinalizer {
				flags.F.FinalizerAdd = true
			} else {
				flags.F.FinalizerAdd = false
			}

			cfgVault := storage.NewFakeConfigMapVault(metav1.NamespaceSystem, uidConfigMapName)
			if !tc.emptyConfigMap {
				if err := cfgVault.Put(storage.UIDDataKey, configMapUID); err != nil {
					t.Fatalf("Failed to insert key: %s, value: %s into Vault: %v", storage.UIDDataKey, configMapUID, cfgVault)
				}
				if err := cfgVault.Put(storage.ProviderDataKey, configMapProviderUID); err != nil {
					t.Fatalf("Failed to insert key: %s, value: %s into Vault: %v", storage.ProviderDataKey, configMapProviderUID, cfgVault)
				}
			}

			clusterName, useCfgMapForUID, err := getClusterUID(kubeClient, cfgVault, tc.clusterName)
			if err != nil {
				t.Fatalf("getClusterUID(_, _, %s) = _, _, %v, want nil; test case: %+v", tc.clusterName, err, tc)
			}
			if diff := cmp.Diff(tc.expectedClusterName, clusterName); diff != "" && tc.expectedClusterName != random {
				t.Fatalf("Got diff for Cluster UID, (-want +got):\n%s", diff)
			}

			fwName, useCfgMap, err := getFirewallName(cfgVault, tc.fwName, clusterName, useCfgMapForUID)
			if err != nil {
				t.Fatalf("getFirewallName(_, _, %s, %s, %t) = _, _, %v, want nil; test case: %+v", tc.fwName, clusterName, useCfgMapForUID, err, tc)
			}
			if useCfgMap != tc.expectedUseCfgMap {
				t.Fatalf("getFirewallName(_, _, %s, %s, %t) = _, %t, nil, want _, %t, nil; test case: %+v", tc.fwName, clusterName, useCfgMapForUID, useCfgMap, tc.expectedUseCfgMap, tc)
			}
			if diff := cmp.Diff(tc.expectedFwName, fwName); diff != "" && tc.expectedFwName != random {
				t.Fatalf("Got diff for Firewall name, (-want +got):\n%s", diff)
			}
		})
	}
}
