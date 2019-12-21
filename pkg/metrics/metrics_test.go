package metrics

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/api/networking/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	testTTL          = int64(10)
	defaultNamespace = "default"
	testServicePorts = []utils.ServicePort{
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "dummy-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			BackendConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					Cdn: &backendconfigv1beta1.CDNConfig{
						Enabled:     true,
						CachePolicy: &backendconfigv1beta1.CacheKeyPolicy{},
					},
					SessionAffinity: &backendconfigv1beta1.SessionAffinityConfig{
						AffinityType:         "GENERATED_COOKIE",
						AffinityCookieTtlSec: &testTTL,
					},
					SecurityPolicy: &backendconfigv1beta1.SecurityPolicyConfig{
						Name: "security-policy-1",
					},
					ConnectionDraining: &backendconfigv1beta1.ConnectionDrainingConfig{
						DrainingTimeoutSec: testTTL,
					},
				},
			},
		},
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "foo-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			NEGEnabled: true,
			BackendConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					Iap: &backendconfigv1beta1.IAPConfig{
						Enabled: true,
					},
					SessionAffinity: &backendconfigv1beta1.SessionAffinityConfig{
						AffinityType:         "CLIENT_IP",
						AffinityCookieTtlSec: &testTTL,
					},
					TimeoutSec: &testTTL,
					CustomRequestHeaders: &backendconfigv1beta1.CustomRequestHeadersConfig{
						Headers: []string{},
					},
				},
			},
		},
		// NEG default backend.
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "dummy-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			NEGEnabled:   true,
			L7ILBEnabled: true,
		},
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "bar-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(5000),
			},
			NEGEnabled:   true,
			L7ILBEnabled: true,
			BackendConfig: &backendconfigv1beta1.BackendConfig{
				Spec: backendconfigv1beta1.BackendConfigSpec{
					Iap: &backendconfigv1beta1.IAPConfig{
						Enabled: true,
					},
					SessionAffinity: &backendconfigv1beta1.SessionAffinityConfig{
						AffinityType:         "GENERATED_COOKIE",
						AffinityCookieTtlSec: &testTTL,
					},
					ConnectionDraining: &backendconfigv1beta1.ConnectionDrainingConfig{
						DrainingTimeoutSec: testTTL,
					},
				},
			},
		},
	}
	ingressStates = []struct {
		desc             string
		ing              *v1beta1.Ingress
		frontendFeatures []Feature
		svcPorts         []utils.ServicePort
		backendFeatures  []Feature
	}{
		{
			"empty spec",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress0",
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled},
			[]utils.ServicePort{},
			nil,
		},
		{
			"http disabled",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress1",
					Annotations: map[string]string{
						allowHTTPKey: "false"},
				},
			},
			[]Feature{ingress, externalIngress},
			[]utils.ServicePort{},
			nil,
		},
		{
			"default backend",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress2",
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled},
			[]utils.ServicePort{testServicePorts[0]},
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"host rule only",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress3",
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
						},
					},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled, hostBasedRouting},
			[]utils.ServicePort{},
			nil,
		},
		{
			"both host and path rules",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress4",
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			[]utils.ServicePort{testServicePorts[1]},
			[]Feature{servicePort, externalServicePort, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"default backend and host rule",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress5",
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			testServicePorts[:2],
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"tls termination with pre-shared certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress6",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				preSharedCertsForTLS, tlsTermination},
			[]utils.ServicePort{testServicePorts[0]},
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with google managed certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress7",
					Annotations: map[string]string{
						managedCertKey: "managed-cert1,managed-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				managedCertsForTLS, tlsTermination},
			[]utils.ServicePort{testServicePorts[0]},
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with pre-shared and google managed certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress8",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						managedCertKey:   "managed-cert1,managed-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				preSharedCertsForTLS, managedCertsForTLS, tlsTermination},
			[]utils.ServicePort{testServicePorts[0]},
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with pre-shared and secret based certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress9",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
					TLS: []v1beta1.IngressTLS{
						{
							Hosts:      []string{"foo.bar"},
							SecretName: "secret-1",
						},
					},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled, hostBasedRouting,
				pathBasedRouting, preSharedCertsForTLS, secretBasedCertsForTLS, tlsTermination},
			[]utils.ServicePort{testServicePorts[1]},
			[]Feature{servicePort, externalServicePort, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"global static ip",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress10",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						staticIPKey:      "10.0.1.2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]Feature{ingress, externalIngress, httpEnabled,
				preSharedCertsForTLS, tlsTermination, staticGlobalIP},
			[]utils.ServicePort{testServicePorts[0]},
			[]Feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"default backend, host rule for internal load-balancer",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress11",
					Annotations: map[string]string{
						ingressClassKey: gceL7ILBIngressClass,
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{
						{
							Host: "bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/bar",
											Backend: v1beta1.IngressBackend{
												ServiceName: "bar-service",
												ServicePort: intstr.FromInt(5000),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]Feature{ingress, internalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			[]utils.ServicePort{testServicePorts[2], testServicePorts[3]},
			[]Feature{servicePort, internalServicePort, neg, cloudIAP,
				cookieAffinity, backendConnectionDraining},
		},
	}
)

func TestFeaturesForIngress(t *testing.T) {
	t.Parallel()
	for _, tc := range ingressStates {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotFrontendFeatures := FeaturesForIngress(tc.ing)
			if diff := cmp.Diff(tc.frontendFeatures, gotFrontendFeatures); diff != "" {
				t.Fatalf("Got diff for frontend features (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeaturesForServicePort(t *testing.T) {
	t.Parallel()
	for _, tc := range ingressStates {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			backendFeatureMap := make(map[Feature]bool)
			var gotBackendFeatures []Feature
			for _, svcPort := range tc.svcPorts {
				for _, feature := range FeaturesForServicePort(svcPort) {
					if backendFeatureMap[feature] {
						continue
					}
					backendFeatureMap[feature] = true
					gotBackendFeatures = append(gotBackendFeatures, feature)
				}
			}
			if diff := cmp.Diff(tc.backendFeatures, gotBackendFeatures); diff != "" {
				t.Fatalf("Got diff for backend features (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComputeMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc               string
		ingressStates      []IngressState
		expectIngressCount map[Feature]int
		expectSvcPortCount map[Feature]int
	}{
		{
			"frontend features only",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
			},
			map[Feature]int{
				backendConnectionDraining: 0,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  0,
				cookieAffinity:            0,
				customRequestHeaders:      0,
				externalIngress:           3,
				httpEnabled:               2,
				hostBasedRouting:          1,
				ingress:                   3,
				internalIngress:           0,
				managedCertsForTLS:        0,
				neg:                       0,
				pathBasedRouting:          0,
				preSharedCertsForTLS:      0,
				secretBasedCertsForTLS:    0,
				staticGlobalIP:            0,
				tlsTermination:            0,
			},
			map[Feature]int{
				backendConnectionDraining: 0,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  0,
				cookieAffinity:            0,
				customRequestHeaders:      0,
				internalServicePort:       0,
				servicePort:               0,
				externalServicePort:       0,
				neg:                       0,
			},
		},
		{
			"features for internal and external load-balancers",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
				NewIngressState(ingressStates[11].ing, ingressStates[11].svcPorts),
			},
			map[Feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      0,
				externalIngress:           3,
				httpEnabled:               3,
				hostBasedRouting:          2,
				ingress:                   4,
				internalIngress:           1,
				managedCertsForTLS:        0,
				neg:                       1,
				pathBasedRouting:          1,
				preSharedCertsForTLS:      0,
				secretBasedCertsForTLS:    0,
				staticGlobalIP:            0,
				tlsTermination:            0,
			},
			map[Feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      0,
				internalServicePort:       2,
				servicePort:               2,
				externalServicePort:       0,
				neg:                       2,
			},
		},
		{
			"frontend and backend features",
			[]IngressState{
				NewIngressState(ingressStates[2].ing, ingressStates[2].svcPorts),
				NewIngressState(ingressStates[4].ing, ingressStates[4].svcPorts),
				NewIngressState(ingressStates[6].ing, ingressStates[6].svcPorts),
				NewIngressState(ingressStates[8].ing, ingressStates[8].svcPorts),
				NewIngressState(ingressStates[10].ing, ingressStates[10].svcPorts),
			},
			map[Feature]int{
				backendConnectionDraining: 4,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                4,
				cloudCDN:                  4,
				cloudIAP:                  1,
				cookieAffinity:            4,
				customRequestHeaders:      1,
				externalIngress:           5,
				httpEnabled:               5,
				hostBasedRouting:          1,
				ingress:                   5,
				internalIngress:           0,
				managedCertsForTLS:        1,
				neg:                       1,
				pathBasedRouting:          1,
				preSharedCertsForTLS:      3,
				secretBasedCertsForTLS:    0,
				staticGlobalIP:            1,
				tlsTermination:            3,
			},
			map[Feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                1,
				cloudCDN:                  1,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      1,
				internalServicePort:       0,
				servicePort:               2,
				externalServicePort:       2,
				neg:                       1,
			},
		},
		{
			"all ingress features",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[2].ing, ingressStates[2].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
				NewIngressState(ingressStates[4].ing, ingressStates[4].svcPorts),
				NewIngressState(ingressStates[5].ing, ingressStates[5].svcPorts),
				NewIngressState(ingressStates[6].ing, ingressStates[6].svcPorts),
				NewIngressState(ingressStates[7].ing, ingressStates[7].svcPorts),
				NewIngressState(ingressStates[8].ing, ingressStates[8].svcPorts),
				NewIngressState(ingressStates[9].ing, ingressStates[9].svcPorts),
				NewIngressState(ingressStates[10].ing, ingressStates[10].svcPorts),
				NewIngressState(ingressStates[11].ing, ingressStates[11].svcPorts),
			},
			map[Feature]int{
				backendConnectionDraining: 7,
				backendTimeout:            3,
				clientIPAffinity:          3,
				cloudArmor:                6,
				cloudCDN:                  6,
				cloudIAP:                  4,
				cookieAffinity:            7,
				customRequestHeaders:      3,
				externalIngress:           11,
				httpEnabled:               11,
				hostBasedRouting:          5,
				ingress:                   12,
				internalIngress:           1,
				managedCertsForTLS:        2,
				neg:                       4,
				pathBasedRouting:          4,
				preSharedCertsForTLS:      4,
				secretBasedCertsForTLS:    1,
				staticGlobalIP:            1,
				tlsTermination:            5,
			},
			map[Feature]int{
				backendConnectionDraining: 2,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                1,
				cloudCDN:                  1,
				cloudIAP:                  2,
				cookieAffinity:            2,
				customRequestHeaders:      1,
				internalServicePort:       2,
				servicePort:               4,
				externalServicePort:       2,
				neg:                       3,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			ingMetrics := NewIngressMetrics()
			for _, ingState := range tc.ingressStates {
				ingKey := fmt.Sprintf("%s/%s", defaultNamespace, ingState.ingress.Name)
				ingMetrics.Set(ingKey, ingState)
			}
			gotIngressCount, gotSvcPortCount := ingMetrics.computeMetrics()
			if diff := cmp.Diff(tc.expectIngressCount, gotIngressCount); diff != "" {
				t.Errorf("Got diff for ingress features count (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectSvcPortCount, gotSvcPortCount); diff != "" {
				t.Fatalf("Got diff for service port features count (-want +got):\n%s", diff)
			}
		})
	}
}
