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

package namer

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newIngress(namespace, name string) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func TestLegacyIngressFrontendNamer(t *testing.T) {
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	ing := newIngress("namespace", "name")
	for _, tc := range []struct {
		prefix string

		lbName              string
		targetHTTPProxy     string
		targetHTTPSProxy    string
		sslCert             string
		forwardingRuleHTTP  string
		forwardingRuleHTTPS string
		urlMap              string
	}{
		{
			"k8s",
			"namespace-name--uid1",
			"k8s-tp-namespace-name--uid1",
			"k8s-tps-namespace-name--uid1",
			"k8s-ssl-9a60a5272f6eee97-%s--uid1",
			"k8s-fw-namespace-name--uid1",
			"k8s-fws-namespace-name--uid1",
			"k8s-um-namespace-name--uid1",
		},
		{
			"mci",
			"namespace-name--uid1",
			"mci-tp-namespace-name--uid1",
			"mci-tps-namespace-name--uid1",
			"mci-ssl-9a60a5272f6eee97-%s--uid1",
			"mci-fw-namespace-name--uid1",
			"mci-fws-namespace-name--uid1",
			"mci-um-namespace-name--uid1",
		},
	} {
		oldNamer := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		fenamer := NewLegacyIngressFrontendNamer(ing, oldNamer)

		tc.sslCert = fmt.Sprintf(tc.sslCert, secretHash)

		if diff := cmp.Diff(tc.lbName, fenamer.GetLbName()); diff != "" {
			t.Errorf("fenamer.GetLbName() mismatch (-want +got):\n%s", diff)
		}
		name := fenamer.TargetProxy(HTTPProtocol)
		if diff := cmp.Diff(tc.targetHTTPProxy, name); diff != "" {
			t.Errorf("fenamer.TargetProxy(HTTPProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.TargetProxy(HTTPSProtocol)
		if diff := cmp.Diff(tc.targetHTTPSProxy, name); diff != "" {
			t.Errorf("fenamer.TargetProxy(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.SSLCertName(secretHash)
		if diff := cmp.Diff(tc.sslCert, name); diff != "" {
			t.Errorf("fenamer.SSLCertName(%q) mismatch (-want +got):\n%s", secretHash, diff)
		}
		name = fenamer.ForwardingRule(HTTPProtocol)
		if diff := cmp.Diff(tc.forwardingRuleHTTP, name); diff != "" {
			t.Errorf("fenamer.ForwardingRule(HTTPProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.ForwardingRule(HTTPSProtocol)
		if diff := cmp.Diff(tc.forwardingRuleHTTPS, name); diff != "" {
			t.Errorf("fenamer.ForwardingRule(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.UrlMap()
		if diff := cmp.Diff(tc.urlMap, name); diff != "" {
			t.Errorf("fenamer.UrlMap() mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestV2IngressFrontendNamer(t *testing.T) {
	secretHash := Hash("test123", 16)
	ing := newIngress("namespace", "name")
	for _, tc := range []struct {
		prefix              string
		lbName              string
		targetHTTPProxy     string
		targetHTTPSProxy    string
		sslCert             string
		forwardingRuleHTTP  string
		forwardingRuleHTTPS string
		urlMap              string
	}{
		{
			"k8s",
			"uid1-namespace-name-e85s5d5h",
			"k8s2-tp-uid1-namespace-name-e85s5d5h",
			"k8s2-ts-uid1-namespace-name-e85s5d5h",
			"k8s2-uid1-mwl6kbmmi3ddkk7p-%s",
			"k8s2-fw-uid1-namespace-name-e85s5d5h",
			"k8s2-fs-uid1-namespace-name-e85s5d5h",
			"k8s2-uid1-namespace-name-e85s5d5h",
		},
		{
			"mci",
			"uid1-namespace-name-e85s5d5h",
			"mci2-tp-uid1-namespace-name-e85s5d5h",
			"mci2-ts-uid1-namespace-name-e85s5d5h",
			"mci2-uid1-mwl6kbmmi3ddkk7p-%s",
			"mci2-fw-uid1-namespace-name-e85s5d5h",
			"mci2-fs-uid1-namespace-name-e85s5d5h",
			"mci2-uid1-namespace-name-e85s5d5h",
		},
	} {
		oldNamer := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		fenamer := NewV2IngressFrontendNamer(ing, oldNamer)

		tc.sslCert = fmt.Sprintf(tc.sslCert, secretHash)

		if diff := cmp.Diff(tc.lbName, fenamer.GetLbName()); diff != "" {
			t.Errorf("fenamer.GetLbName() mismatch (-want +got):\n%s", diff)
		}
		name := fenamer.TargetProxy(HTTPProtocol)
		if diff := cmp.Diff(tc.targetHTTPProxy, name); diff != "" {
			t.Errorf("fenamer.TargetProxy(HTTPProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.TargetProxy(HTTPSProtocol)
		if diff := cmp.Diff(tc.targetHTTPSProxy, name); diff != "" {
			t.Errorf("fenamer.TargetProxy(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.SSLCertName(secretHash)
		if diff := cmp.Diff(tc.sslCert, name); diff != "" {
			t.Errorf("fenamer.SSLCertName(%q) mismatch (-want +got):\n%s", secretHash, diff)
		}
		name = fenamer.ForwardingRule(HTTPProtocol)
		if diff := cmp.Diff(tc.forwardingRuleHTTP, name); diff != "" {
			t.Errorf("fenamer.ForwardingRule(HTTPProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.ForwardingRule(HTTPSProtocol)
		if diff := cmp.Diff(tc.forwardingRuleHTTPS, name); diff != "" {
			t.Errorf("fenamer.ForwardingRule(HTTPSProtocol) mismatch (-want +got):\n%s", diff)
		}
		name = fenamer.UrlMap()
		if diff := cmp.Diff(tc.urlMap, name); diff != "" {
			t.Errorf("fenamer.UrlMap() mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestV2IngressFrontendNamerIsCertUsedForLB(t *testing.T) {
	cases := map[string]struct {
		prefix      string
		ing         *v1beta1.Ingress
		secretValue string
	}{
		"short ingress name": {
			prefix:      "k8s",
			ing:         newIngress("default", "my-ingress"),
			secretValue: "test123321test",
		},
		"long ingress name": {
			prefix:      "k8s",
			ing:         newIngress("a-very-long-and-useless-namespace-value", "a-very-long-and-nondescript-ingress-name"),
			secretValue: "test123321test",
		},
		"long ingress name with mci prefix": {
			prefix:      "mci",
			ing:         newIngress("a-very-long-and-useless-namespace-value", "a-very-long-and-nondescript-ingress-name"),
			secretValue: "test123321test",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			oldNamer := NewNamerWithPrefix(tc.prefix, "cluster-uid", "fw1")
			fenamer := NewV2IngressFrontendNamer(tc.ing, oldNamer)

			secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.secretValue)))[:16]

			certName := fenamer.SSLCertName(secretHash)
			if v := fenamer.IsCertUsedForLB(certName); !v {
				t.Errorf("fenamer.IsCertUsedForLB(%q) = %t, want true", certName, v)
			}
		})
	}
}
