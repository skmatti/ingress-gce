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

package loadbalancers

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	newnamer "k8s.io/ingress-gce/pkg/utils/namer"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	testClusterName = "0123456789abcedf"

	// resourceLeakLimit is the limit when ingress namespace and name are too long and ingress
	// GC will leak the LB resources because the cluster uid got truncated.
	resourceLeakLimit = 49
)

var longName = strings.Repeat("0123456789", 10)

func TestGC(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	cloud := l7sPool.cloud
	testCases := []struct {
		desc      string
		ingresses []*v1beta1.Ingress
		gcpLBs    []string
		expectLBs []string
	}{
		{
			desc:      "empty",
			ingresses: []*v1beta1.Ingress{},
			gcpLBs:    []string{},
			expectLBs: []string{},
		},
		{
			desc: "remove all lbs",
			ingresses: []*v1beta1.Ingress{
				createIngress(longName[:10], longName[:10]),
				createIngress(longName[:20], longName[:20]),
				createIngress(longName[:20], longName[:27]),
			},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
			},
			expectLBs: []string{},
		},
		{
			// resource leakage is due to the GC logic is doing naming matching.
			// if the name is too long, it truncates the suffix which contains the cluster uid
			// if cluster uid section of a name is too short or completely truncated,
			// ingress controller will leak the resource instead.
			desc: "remove all lbs but with leaks",
			ingresses: []*v1beta1.Ingress{
				createIngress(longName[:10], longName[:10]),
				createIngress(longName[:20], longName[:20]),
				createIngress(longName[:20], longName[:27]),
				createIngress(longName[:21], longName[:27]),
				createIngress(longName[:22], longName[:27]),
				createIngress(longName[:50], longName[:30]),
			},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName[:22], longName[:27]),
				generateKey(longName[:50], longName[:30]),
			},
			expectLBs: []string{
				generateKey(longName[:21], longName[:27]),
				generateKey(longName[:22], longName[:27]),
				generateKey(longName[:50], longName[:30]),
			},
		},
		{
			desc: "no LB exists",
			ingresses: []*v1beta1.Ingress{
				createIngress(longName[:10], longName[:10]),
				createIngress(longName[:20], longName[:20]),
				createIngress(longName[:20], longName[:27]),
				createIngress(longName[:50], longName[:30]),
			},
			gcpLBs:    []string{},
			expectLBs: []string{},
		},
		{
			desc:      "no LB get GCed",
			ingresses: []*v1beta1.Ingress{},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:30]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:30]),
				generateKey(longName, longName),
			},
		},
		{
			desc: "some LB get GCed",
			ingresses: []*v1beta1.Ingress{
				createIngress(longName[:20], longName[:20]),
				createIngress(longName[:20], longName[:27]),
			},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName, longName),
			},
		},
		{
			desc: "some LB get GCed and some leaked",
			ingresses: []*v1beta1.Ingress{
				createIngress(longName[:20], longName[:20]),
				createIngress(longName[:20], longName[:27]),
				createIngress(longName[:21], longName[:27]),
			},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName, longName),
			},
		},
	}

	otherNamer := newnamer.NewNamer("clusteruid", "fw1")
	otherKeys := []string{
		"a/a",
		"namespace/name",
		generateKey(longName[:10], longName[:10]),
		generateKey(longName[:20], longName[:20]),
	}

	versions := features.GAResourceVersions

	for _, key := range otherKeys {
		createFakeLoadbalancer(cloud, otherNamer, key, versions, defaultScope)
	}

	for _, tc := range testCases {
		for _, key := range tc.gcpLBs {
			createFakeLoadbalancer(l7sPool.cloud, l7sPool.namer, key, versions, defaultScope)
		}

		err := l7sPool.GC(tc.ingresses)
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		// check if other LB are not deleted
		for _, key := range otherKeys {
			if err := checkFakeLoadBalancer(l7sPool.cloud, otherNamer, key, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q and ing %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if the total number of url maps are expected
		urlMaps, _ := l7sPool.cloud.ListURLMaps()
		if len(urlMaps) != len(tc.expectLBs)+len(otherKeys) {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, len(tc.expectLBs)+len(otherKeys), len(urlMaps))
		}

		// check if the ones that are expected to be GC is actually GCed.
		expectRemovedLBs := sets.NewString(tc.gcpLBs...).Difference(sets.NewString(tc.expectLBs...)).Difference(sets.NewString(ingressesToLbNames(tc.ingresses)...))
		for _, key := range expectRemovedLBs.List() {
			if err := checkFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key, versions, defaultScope, false); err != nil {
				t.Errorf("For case %q and ing %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if all expected LBs exists
		for _, key := range tc.expectLBs {
			if err := checkFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q and ing %q, do not expect err: %v", tc.desc, key, err)
			}
			removeFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key, versions, defaultScope)
		}
	}
}

func TestDoNotGCWantedLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	namer := l7sPool.namer
	type testCase struct {
		desc string
		ing  *v1beta1.Ingress
	}
	testCases := []testCase{}

	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), createIngress(generateKeyWithLength(i))})
	}

	numOfExtraUrlMap := 2
	l7sPool.cloud.CreateURLMap(&compute.UrlMap{Name: "random--name1"})
	l7sPool.cloud.CreateURLMap(&compute.UrlMap{Name: "k8s-random-random--1111111111111111"})

	versions := features.GAResourceVersions

	for _, tc := range testCases {
		ingKey := generateKey(tc.ing.Namespace, tc.ing.Name)
		createFakeLoadbalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope)
		err := l7sPool.GC([]*v1beta1.Ingress{})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if err := checkFakeLoadBalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope, true); err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}
		urlMaps, _ := l7sPool.cloud.ListURLMaps()
		if len(urlMaps) != 1+numOfExtraUrlMap {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, 1+numOfExtraUrlMap, len(urlMaps))
		}
		removeFakeLoadBalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope)
	}
}

// This should not leak at all, but verfies existing behavior
// TODO: remove this test after the GC resource leaking is fixed.
func TestGCToLeakLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	namer := l7sPool.namer
	type testCase struct {
		desc string
		ing  *v1beta1.Ingress
	}
	testCases := []testCase{}
	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), createIngress(generateKeyWithLength(i))})
	}

	versions := features.GAResourceVersions

	for _, tc := range testCases {
		ingKey := generateKey(tc.ing.Namespace, tc.ing.Name)
		createFakeLoadbalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope)
		err := l7sPool.GC([]*v1beta1.Ingress{tc.ing})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if len(ingKey) >= resourceLeakLimit {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
			removeFakeLoadBalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope)
		} else {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, ingKey, versions, defaultScope, false); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
		}
	}
}

func newTestLoadBalancerPool() LoadBalancerPool {
	namer := newnamer.NewNamer(testClusterName, "fw1")
	fakeGCECloud := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	ctx := &context.ControllerContext{}
	return NewLoadBalancerPool(fakeGCECloud, namer, ctx)
}

func createFakeLoadbalancer(cloud *gce.Cloud, namer *newnamer.Namer, lbKey string, versions *features.ResourceVersions, scope meta.KeyType) {
	lbName := namer.LoadBalancer(lbKey)
	key, _ := composite.CreateKey(cloud, "", scope)

	key.Name = namer.ForwardingRule(lbName, newnamer.HTTPProtocol)
	composite.CreateForwardingRule(cloud, key, &composite.ForwardingRule{Name: key.Name, Version: versions.ForwardingRule})

	key.Name = namer.TargetProxy(lbName, newnamer.HTTPProtocol)
	composite.CreateTargetHttpProxy(cloud, key, &composite.TargetHttpProxy{Name: key.Name, Version: versions.TargetHttpProxy})

	key.Name = namer.UrlMap(lbName)
	composite.CreateUrlMap(cloud, key, &composite.UrlMap{Name: key.Name, Version: versions.UrlMap})

	cloud.ReserveGlobalAddress(&compute.Address{Name: namer.ForwardingRule(lbName, newnamer.HTTPProtocol)})

}

func removeFakeLoadBalancer(cloud *gce.Cloud, namer *newnamer.Namer, lbKey string, versions *features.ResourceVersions, scope meta.KeyType) {
	lbName := namer.LoadBalancer(lbKey)

	key, _ := composite.CreateKey(cloud, "", scope)
	key.Name = namer.ForwardingRule(lbName, newnamer.HTTPProtocol)
	composite.DeleteForwardingRule(cloud, key, versions.ForwardingRule)

	key.Name = namer.TargetProxy(lbName, newnamer.HTTPProtocol)
	composite.DeleteTargetHttpProxy(cloud, key, versions.TargetHttpProxy)

	key.Name = namer.UrlMap(lbName)
	composite.DeleteUrlMap(cloud, key, versions.UrlMap)

	cloud.DeleteGlobalAddress(namer.ForwardingRule(lbName, newnamer.HTTPProtocol))
}

func checkFakeLoadBalancer(cloud *gce.Cloud, namer *newnamer.Namer, lbKey string, versions *features.ResourceVersions, scope meta.KeyType, expectPresent bool) error {
	var err error
	lbName := namer.LoadBalancer(lbKey)
	key, _ := composite.CreateKey(cloud, namer.ForwardingRule(lbName, newnamer.HTTPProtocol), scope)

	_, err = composite.GetForwardingRule(cloud, key, versions.ForwardingRule)
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect GlobalForwardingRule %q to not present: %v", key, err)
	}

	key.Name = namer.TargetProxy(lbName, newnamer.HTTPProtocol)
	_, err = composite.GetTargetHttpProxy(cloud, key, versions.TargetHttpProxy)
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect TargetHTTPProxy %q to not present: %v", key, err)
	}

	key.Name = namer.UrlMap(lbName)
	_, err = composite.GetUrlMap(cloud, key, versions.UrlMap)
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect URLMap %q to not present: %v", key, err)
	}
	return nil
}

func generateKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func generateKeyWithLength(length int) (string, string) {
	if length < 3 {
		length = 3
	}
	if length > 201 {
		length = 201
	}
	length = length - 1
	return longName[:length/2], longName[:length-length/2]
}

func ingressesToLbNames(ings []*v1beta1.Ingress) []string {
	var lbNames []string
	for _, ing := range ings {
		lbNames = append(lbNames, generateKey(ing.Namespace, ing.Name))
	}
	return lbNames
}

func createIngress(namespace, name string) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}
