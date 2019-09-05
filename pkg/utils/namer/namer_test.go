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

package namer

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

const (
	clusterId = "0123456789abcdef"
)

var (
	longString string
)

func init() {
	for i := 0; i < 100; i++ {
		longString += "x"
	}
}

func TestTruncate(t *testing.T) {
	for i := 0; i < len(longString); i++ {
		s := truncate(longString[:i])
		if len(s) > 63 {
			t.Errorf("truncate(longString[:%v]) = %q, length was greater than 63", i, s)
		}
	}
}

func TestNamerUID(t *testing.T) {
	const uid = "cluster-uid"
	namer1 := NewNamer(uid, "cluster-fw")
	if namer1.UID() != uid {
		t.Errorf("namer1.UID() = %q, want %q", namer1.UID(), uid)
	}

	for _, tc := range []struct {
		uidToSet string
		want     string
	}{
		{"", ""},
		{"--", ""},
		{"--my-uid", "my-uid"},
		{"xxx--yyyyy", "yyyyy"},
		{"xxx--yyyyy--zzz", "zzz"},
		{"xxx--yyyyy--zzz--abc", "abc"},
	} {
		namer1.SetUID(tc.uidToSet)
		if namer1.UID() != tc.want {
			t.Errorf("namer1.UID() = %q, want %q", namer1.UID(), tc.want)
		}
	}
}

func TestNamerFirewall(t *testing.T) {
	const uid = "cluster-uid"
	const fw1 = "fw1"
	namer1 := NewNamer(uid, fw1)
	if namer1.Firewall() != fw1 {
		t.Errorf("namer1.Firewall() = %q, want %q", namer1.Firewall(), fw1)
	}

	namer1 = NewNamer(uid, "")
	if namer1.Firewall() != uid {
		t.Errorf("when initial firewall is empty, namer1.Firewall() = %q, want %q", namer1.Firewall(), uid)
	}

	const fw2 = "fw2"
	namer1.SetFirewall(fw2)
	if namer1.Firewall() != fw2 {
		t.Errorf("namer1.Firewall() = %q, want %q", namer1.Firewall(), fw2)
	}
}

func TestNamerParseName(t *testing.T) {
	const uid = "uid1"
	namer1 := NewNamer(uid, "fw1")
	lbName := namer1.LoadBalancer("key1")
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	for _, tc := range []struct {
		in   string
		want *NameComponents
	}{
		{"", &NameComponents{}}, // TODO: this should really be a parse error.
		{namer1.IGBackend(80), &NameComponents{ClusterName: uid, Resource: "be"}},
		{namer1.InstanceGroup(), &NameComponents{ClusterName: uid, Resource: "ig"}},
		{namer1.TargetProxy(lbName, HTTPProtocol), &NameComponents{ClusterName: uid, Resource: "tp"}},
		{namer1.TargetProxy(lbName, HTTPSProtocol), &NameComponents{ClusterName: uid, Resource: "tps"}},
		{namer1.SSLCertName("default/my-ing", secretHash), &NameComponents{ClusterName: uid, Resource: "ssl"}},
		{namer1.SSLCertName("default/my-ing", secretHash), &NameComponents{ClusterName: uid, Resource: "ssl"}},
		{namer1.ForwardingRule(lbName, HTTPProtocol), &NameComponents{ClusterName: uid, Resource: "fw"}},
		{namer1.ForwardingRule(lbName, HTTPSProtocol), &NameComponents{ClusterName: uid, Resource: "fws"}},
		{namer1.UrlMap(lbName), &NameComponents{ClusterName: uid, Resource: "um", LbName: "key1"}},
	} {
		nc := namer1.ParseName(tc.in)
		if *nc != *tc.want {
			t.Errorf("namer1.ParseName(%q) = %+v, want %+v", tc.in, nc, *tc.want)
		}
	}
}

func TestNameBelongsToCluster(t *testing.T) {
	const uid = "0123456789abcdef"
	// string with 10 characters
	longKey := "0123456789"
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	for _, prefix := range []string{defaultPrefix, "mci"} {
		namer1 := NewNamerWithPrefix(prefix, uid, "fw1")
		lbName := namer1.LoadBalancer("key1")
		// longLBName with 40 characters. Triggers truncation
		longLBName := namer1.LoadBalancer(strings.Repeat(longKey, 4))
		// Positive cases.
		for _, tc := range []string{
			// short names
			namer1.IGBackend(80),
			namer1.InstanceGroup(),
			namer1.TargetProxy(lbName, HTTPProtocol),
			namer1.TargetProxy(lbName, HTTPSProtocol),
			namer1.SSLCertName("default/my-ing", secretHash),
			namer1.ForwardingRule(lbName, HTTPProtocol),
			namer1.ForwardingRule(lbName, HTTPSProtocol),
			namer1.UrlMap(lbName),
			namer1.NEG("ns", "n", int32(80)),
			// long names that are truncated
			namer1.TargetProxy(longLBName, HTTPProtocol),
			namer1.TargetProxy(longLBName, HTTPSProtocol),
			namer1.SSLCertName(longLBName, secretHash),
			namer1.ForwardingRule(longLBName, HTTPProtocol),
			namer1.ForwardingRule(longLBName, HTTPSProtocol),
			namer1.UrlMap(longLBName),
			namer1.NEG(strings.Repeat(longKey, 3), strings.Repeat(longKey, 3), int32(88888)),
		} {
			if !namer1.NameBelongsToCluster(tc) {
				t.Errorf("namer1.NameBelongsToCluster(%q) = false, want true", tc)
			}
		}
	}

	// Negative cases.
	namer1 := NewNamer(uid, "fw1")
	// longLBName with 60 characters. Triggers truncation to eliminate cluster name suffix
	longLBName := namer1.LoadBalancer(strings.Repeat(longKey, 6))
	for _, tc := range []string{
		"",
		"invalid",
		"not--the-right-uid",
		namer1.TargetProxy(longLBName, HTTPProtocol),
		namer1.TargetProxy(longLBName, HTTPSProtocol),
		namer1.ForwardingRule(longLBName, HTTPProtocol),
		namer1.ForwardingRule(longLBName, HTTPSProtocol),
		namer1.UrlMap(longLBName),
	} {
		if namer1.NameBelongsToCluster(tc) {
			t.Errorf("namer1.NameBelongsToCluster(%q) = true, want false", tc)
		}
	}
}

func TestNamerBackend(t *testing.T) {
	for _, tc := range []struct {
		desc string
		uid  string
		port int64
		want string
	}{
		{desc: "default", port: 80, want: "k8s-be-80--uid1"},
		{desc: "uid", uid: "uid2", port: 80, want: "k8s-be-80--uid2"},
		{desc: "port", port: 8080, want: "k8s-be-8080--uid1"},
		{
			desc: "truncation",
			uid:  longString,
			port: 8080,
			want: "k8s-be-8080--xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx0",
		},
	} {
		namer1 := NewNamer("uid1", "fw1")
		if tc.uid != "" {
			namer1.SetUID(tc.uid)
		}
		name := namer1.IGBackend(tc.port)
		if name != tc.want {
			t.Errorf("%s: namer1.Backend() = %q, want %q", tc.desc, name, tc.want)
		}
	}
	// Prefix.
	namer1 := NewNamerWithPrefix("mci", "uid1", "fw1")
	name := namer1.IGBackend(80)
	const want = "mci-be-80--uid1"
	if name != want {
		t.Errorf("with prefix = %q, namer1.Backend(80) = %q, want %q", "mci", name, want)
	}
}

func TestBackendPort(t *testing.T) {
	namer1 := NewNamer("uid1", "fw1")
	for _, tc := range []struct {
		in    string
		port  string
		valid bool
	}{
		{"", "", false},
		{"k8s-be-80--uid1", "80", true},
		{"k8s-be-8080--uid1", "8080", true},
		{"k8s-be-port1--uid1", "8080", false},
	} {
		port, err := namer1.IGBackendPort(tc.in)
		if err != nil {
			if tc.valid {
				t.Errorf("namer1.BackendPort(%q) = _, %v, want _, nil", tc.in, err)
			}
			continue
		}
		if !tc.valid {
			t.Errorf("namer1.BackendPort(%q) = _, nil, want error", tc.in)
			continue
		}
		if port != tc.port {
			t.Errorf("namer1.BackendPort(%q) = %q, nil, want %q, nil", tc.in, port, tc.port)
		}
	}
}

func TestIsSSLCert(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		in     string
		want   bool
	}{
		{defaultPrefix, "", false},
		{defaultPrefix, "k8s-ssl-foo--uid", true},
		{defaultPrefix, "k8s-tp-foo--uid", false},
		{"mci", "mci-ssl-foo--uid", true},
	} {
		namer1 := NewNamerWithPrefix(tc.prefix, "uid", "fw")
		res := namer1.IsLegacySSLCert("foo", tc.in)
		if res != tc.want {
			t.Errorf("with prefix = %q, namer1.IsLegacySSLCert(%q) = %v, want %v", tc.prefix, tc.in, res, tc.want)
		}
	}
}

func TestNamedPort(t *testing.T) {
	namer1 := NewNamer("uid1", "fw1")
	name := namer1.NamedPort(80)
	const want = "port80"
	if name != want {
		t.Errorf("namer1.NamedPort(80) = %q, want %q", name, want)
	}
}

func TestNamerInstanceGroup(t *testing.T) {
	namer1 := NewNamer("uid1", "fw1")
	name := namer1.InstanceGroup()
	if name != "k8s-ig--uid1" {
		t.Errorf("namer1.InstanceGroup() = %q, want %q", name, "k8s-ig--uid1")
	}
	// Prefix.
	namer1 = NewNamerWithPrefix("mci", "uid1", "fw1")
	name = namer1.InstanceGroup()
	if name != "mci-ig--uid1" {
		t.Errorf("namer1.InstanceGroup() = %q, want %q", name, "mci-ig--uid1")
	}
}

func TestNamerFirewallRule(t *testing.T) {
	namer1 := NewNamer("uid1", "fw1")
	name := namer1.FirewallRule()
	if name != "k8s-fw-l7--fw1" {
		t.Errorf("namer1.FirewallRule() = %q, want %q", name, "k8s-fw-l7--fw1")
	}
}

func TestNamerLoadBalancer(t *testing.T) {
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
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
			"key1--uid1",
			"k8s-tp-key1--uid1",
			"k8s-tps-key1--uid1",
			"k8s-ssl-%s-%s--uid1",
			"k8s-fw-key1--uid1",
			"k8s-fws-key1--uid1",
			"k8s-um-key1--uid1",
		},
		{
			"mci",
			"key1--uid1",
			"mci-tp-key1--uid1",
			"mci-tps-key1--uid1",
			"mci-ssl-%s-%s--uid1",
			"mci-fw-key1--uid1",
			"mci-fws-key1--uid1",
			"mci-um-key1--uid1",
		},
	} {
		namer1 := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		lbName := namer1.LoadBalancer("key1")
		// namespaceHash is calculated the same way as cert hash
		namespaceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(lbName)))[:16]
		tc.sslCert = fmt.Sprintf(tc.sslCert, namespaceHash, secretHash)

		if lbName != tc.lbName {
			t.Errorf("lbName = %q, want %q", lbName, "key1--uid1")
		}
		var name string
		name = namer1.TargetProxy(lbName, HTTPProtocol)
		if name != tc.targetHTTPProxy {
			t.Errorf("namer1.TargetProxy(%q, HTTPProtocol) = %q, want %q", lbName, name, tc.targetHTTPProxy)
		}
		name = namer1.TargetProxy(lbName, HTTPSProtocol)
		if name != tc.targetHTTPSProxy {
			t.Errorf("namer1.TargetProxy(%q, HTTPSProtocol) = %q, want %q", lbName, name, tc.targetHTTPSProxy)
		}
		name = namer1.SSLCertName(lbName, secretHash)
		if name != tc.sslCert {
			t.Errorf("namer1.SSLCertName(%q, true) = %q, want %q", lbName, name, tc.sslCert)
		}
		name = namer1.ForwardingRule(lbName, HTTPProtocol)
		if name != tc.forwardingRuleHTTP {
			t.Errorf("namer1.ForwardingRule(%q, HTTPProtocol) = %q, want %q", lbName, name, tc.forwardingRuleHTTP)
		}
		name = namer1.ForwardingRule(lbName, HTTPSProtocol)
		if name != tc.forwardingRuleHTTPS {
			t.Errorf("namer1.ForwardingRule(%q, HTTPSProtocol) = %q, want %q", lbName, name, tc.forwardingRuleHTTPS)
		}
		name = namer1.UrlMap(lbName)
		if name != tc.urlMap {
			t.Errorf("namer1.UrlMap(%q) = %q, want %q", lbName, name, tc.urlMap)
		}
	}
}

// Ensure that a valid cert name is created if clusterName is empty.
func TestNamerSSLCertName(t *testing.T) {
	secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test123")))[:16]
	namer1 := NewNamerWithPrefix("k8s", "", "fw1")
	lbName := namer1.LoadBalancer("key1")
	certName := namer1.SSLCertName(lbName, secretHash)
	if strings.HasSuffix(certName, clusterNameDelimiter) {
		t.Errorf("Invalid Cert name %s ending with %s", certName, clusterNameDelimiter)
	}
}

func TestNamerIsCertUsedForLB(t *testing.T) {
	cases := map[string]struct {
		prefix      string
		ingName     string
		secretValue string
	}{
		"short ingress name": {
			prefix:      "k8s",
			ingName:     "default/my-ingress",
			secretValue: "test123321test",
		},
		"long ingress name": {
			prefix:      "k8s",
			ingName:     "a-very-long-and-useless-namespace-value/a-very-long-and-nondescript-ingress-name",
			secretValue: "test123321test",
		},
		"long ingress name with mci prefix": {
			prefix:      "mci",
			ingName:     "a-very-long-and-useless-namespace-value/a-very-long-and-nondescript-ingress-name",
			secretValue: "test123321test",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			namer1 := NewNamerWithPrefix(tc.prefix, "cluster-uid", "fw1")
			lbName := namer1.LoadBalancer(tc.ingName)
			secretHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.secretValue)))[:16]

			certName := namer1.SSLCertName(lbName, secretHash)
			if v := namer1.IsCertUsedForLB(lbName, certName); !v {
				t.Errorf("namer1.IsCertUsedForLB(%q, %q) = %v, want %v", lbName, certName, v, true)
			}
		})
	}
}

func TestNamerNEG(t *testing.T) {
	longstring := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc      string
		namespace string
		name      string
		port      int32
		expect    string
	}{
		{
			"simple case",
			"namespace",
			"name",
			80,
			"k8s1-01234567-namespace-name-80-5104b449",
		},
		{
			"63 characters",
			longstring[:10],
			longstring[:10],
			1234567890,
			"k8s1-01234567-0123456789-0123456789-1234567890-ed141b14",
		},
		{
			"long namespace",
			longstring,
			"0",
			0,
			"k8s1-01234567-0123456789012345678901234567890123456-0--72142e04",
		},

		{
			"long name and namespace",
			longstring,
			longstring,
			0,
			"k8s1-01234567-0123456789012345678-0123456789012345678--9129e3d2",
		},
		{
			"long name, namespace and port",
			longstring,
			longstring[:40],
			2147483647,
			"k8s1-01234567-0123456789012345678-0123456789012345-214-ed1f2a2f",
		},
	}

	namer1 := NewNamer(clusterId, "")
	for _, tc := range testCases {
		res := namer1.NEG(tc.namespace, tc.name, tc.port)
		if len(res) > 63 {
			t.Errorf("%s: got len(res) == %v, want <= 63", tc.desc, len(res))
		}
		if res != tc.expect {
			t.Errorf("%s: got %q, want %q", tc.desc, res, tc.expect)
		}
	}

	// Different prefix.
	namer1 = NewNamerWithPrefix("mci", clusterId, "fw")
	name := namer1.NEG("ns", "svc", 80)
	const want = "mci1-01234567-ns-svc-80-4890871b"
	if name != want {
		t.Errorf(`with prefix %q, namer1.NEG("ns", "svc", 80) = %q, want %q`, "mci", name, want)
	}
}

func TestIsNEG(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		in     string
		want   bool
	}{
		{defaultPrefix, "", false},
		{defaultPrefix, "k8s-tp-key1--uid1", false},
		{defaultPrefix, "k8s1-uid1-namespace-name-80-1e047e33", true},
		{"mci", "mci1-uid1-ns-svc-port-16c06497", true},
		{defaultPrefix, "k8s1-uid1234567890123-namespace-name-80-2d8100t5", true},
		{defaultPrefix, "k8s1-uid12345-namespace-name-80-1e047e33", true},
		{defaultPrefix, "k8s1-uid12345-ns-svc-port-16c06497", true},
		{defaultPrefix, "k8s1-wronguid-namespace-name-80-1e047e33", false},
		{defaultPrefix, "k8s-be-80--uid1", false},
		{defaultPrefix, "k8s-ssl-foo--uid", false},
		{defaultPrefix, "invalidk8sresourcename", false},
	} {
		namer1 := NewNamerWithPrefix(tc.prefix, "uid1", "fw1")
		res := namer1.IsNEG(tc.in)
		if res != tc.want {
			t.Errorf("with prefix %q, namer1.IsNEG(%q) = %v, want %v", tc.prefix, tc.in, res, tc.want)
		}
	}
}
