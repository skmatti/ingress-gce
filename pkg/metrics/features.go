package metrics

import (
	"fmt"
	"strconv"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

type Feature string

func (f Feature) String() string {
	return string(f)
}

const (
	// allowHTTPKey tells the Ingress controller to allow/block HTTP access.
	allowHTTPKey         = "kubernetes.io/ingress.allow-http"
	ingressClassKey      = "kubernetes.io/ingress.class"
	gceIngressClass      = "gce"
	gceMultiIngressClass = "gce-multi-cluster"
	gceL7ILBIngressClass = "gce-internal"
	// preSharedCertKey represents the specific pre-shared SSL
	// certificate for the Ingress controller to use.
	preSharedCertKey = "ingress.gcp.kubernetes.io/pre-shared-cert"
	managedCertKey   = "networking.gke.io/managed-certificates"
	// staticIPKey is the annotation key used by controller to record GCP static ip.
	staticIPKey = "ingress.kubernetes.io/static-ip"

	ingress                = Feature("Ingress")
	externalIngress        = Feature("ExternalIngress")
	internalIngress        = Feature("InternalIngress")
	httpEnabled            = Feature("HTTPEnabled")
	hostBasedRouting       = Feature("HostBasedRouting")
	pathBasedRouting       = Feature("PathBasedRouting")
	tlsTermination         = Feature("TLSTermination")
	secretBasedCertsForTLS = Feature("SecretBasedCertsForTLS")
	preSharedCertsForTLS   = Feature("PreSharedCertsForTLS")
	managedCertsForTLS     = Feature("ManagedCertsForTLS")
	staticGlobalIP         = Feature("StaticGlobalIP")

	servicePort               = Feature("L7LBServicePort")
	externalServicePort       = Feature("L7XLBServicePort")
	internalServicePort       = Feature("L7ILBServicePort")
	neg                       = Feature("NEG")
	cloudCDN                  = Feature("CloudCDN")
	cloudArmor                = Feature("CloudArmor")
	cloudIAP                  = Feature("CloudIAP")
	backendTimeout            = Feature("BackendTimeout")
	backendConnectionDraining = Feature("BackendConnectionDraining")
	clientIPAffinity          = Feature("ClientIPAffinity")
	cookieAffinity            = Feature("CookieAffinity")
	customRequestHeaders      = Feature("CustomRequestHeaders")
)

// FeaturesForIngress returns the list of features for given ingress.
func FeaturesForIngress(ing *v1beta1.Ingress) []Feature {
	features := []Feature{ingress}

	ingKey := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	klog.V(4).Infof("Listing features for Ingress %s", ingKey)
	ingAnnotations := ing.Annotations

	// Determine the type of ingress based on ingress class.
	ingClass := ingAnnotations[ingressClassKey]
	klog.V(6).Infof("Ingress class value for ingress %s: %s", ingKey, ingClass)
	switch ingClass {
	case "", gceIngressClass, gceMultiIngressClass:
		features = append(features, externalIngress)
	case gceL7ILBIngressClass:
		features = append(features, internalIngress)
	}

	// Determine if http is enabled.
	if val, ok := ingAnnotations[allowHTTPKey]; !ok {
		klog.V(6).Infof("Annotation %s does not exist for ingress %s", allowHTTPKey, ingKey)
		features = append(features, httpEnabled)
	} else {
		klog.V(6).Infof("User specified value for annotation %s on ingress %s: %s", allowHTTPKey, ingKey, val)
		v, err := strconv.ParseBool(val)
		if err != nil {
			klog.Errorf("Failed to parse %s for annotation %s on ingress %s", val, allowHTTPKey, ingKey)
		}
		if err == nil && v {
			features = append(features, httpEnabled)
		}
	}

	// An ingress without a host or http-path is ignored.
	hostBased, pathBased := false, false
	if len(ing.Spec.Rules) == 0 {
		klog.V(6).Infof("Neither host-based nor path-based routing rules are setup for ingress %s", ingKey)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP != nil && len(rule.HTTP.Paths) > 0 {
			klog.V(6).Infof("User specified http paths for ingress %s: %v", ingKey, rule.HTTP.Paths)
			pathBased = true
		}
		if rule.Host != "" {
			klog.V(6).Infof("User specified host for ingress %s: %v", ingKey, rule.Host)
			hostBased = true
		}
		if pathBased && hostBased {
			break
		}
	}
	if hostBased {
		features = append(features, hostBasedRouting)
	}
	if pathBased {
		features = append(features, pathBasedRouting)
	}

	// SSL certificate based features.
	sslConfigured := false
	if val, ok := ingAnnotations[preSharedCertKey]; ok {
		klog.V(6).Infof("Specified pre-shared certs for ingress %s: %v", ingKey, val)
		sslConfigured = true
		features = append(features, preSharedCertsForTLS)
	}
	if val, ok := ingAnnotations[managedCertKey]; ok {
		klog.V(6).Infof("Specified google managed certs for ingress %s: %v", ingKey, val)
		sslConfigured = true
		features = append(features, managedCertsForTLS)
	}
	if hasSecretBasedCerts(ing) {
		sslConfigured = true
		features = append(features, secretBasedCertsForTLS)
	}
	if sslConfigured {
		klog.V(6).Infof("TLS termination is configured for ingress %s", ingKey)
		features = append(features, tlsTermination)
	}

	// Both user specified and ingress controller managed global static ips are reported.
	if val, ok := ingAnnotations[staticIPKey]; ok && val != "" {
		klog.V(6).Infof("Specified static for ingress %s: %s", ingKey, val)
		features = append(features, staticGlobalIP)
	}
	klog.V(4).Infof("Features for ingress %s/%s: %v", ing.Namespace, ing.Name, features)
	return features
}

// hasSecretBasedCerts returns true if ingress spec contains a secret based cert.
func hasSecretBasedCerts(ing *v1beta1.Ingress) bool {
	for _, tlsSecret := range ing.Spec.TLS {
		if tlsSecret.SecretName == "" {
			continue
		}
		klog.V(6).Infof("User specified secret for ingress %s/%s: %s", ing.Namespace, ing.Name, tlsSecret.SecretName)
		return true
	}
	return false
}

// FeaturesForServicePort returns the list of features for given service port.
func FeaturesForServicePort(sp utils.ServicePort) []Feature {
	features := []Feature{servicePort}
	svcPortKey := newServicePortKey(sp).string()
	klog.V(4).Infof("Listing features for service port %s", svcPortKey)
	if sp.L7ILBEnabled {
		klog.V(6).Infof("L7 ILB is enabled for service port %s", svcPortKey)
		features = append(features, internalServicePort)
	} else {
		features = append(features, externalServicePort)
	}
	if sp.NEGEnabled {
		klog.V(6).Infof("NEG is enabled for service port %s", svcPortKey)
		features = append(features, neg)
	}
	if sp.BackendConfig == nil {
		klog.V(4).Infof("Features for Service port %s: %v", svcPortKey, features)
		return features
	}

	beConfig := fmt.Sprintf("%s/%s", sp.BackendConfig.Namespace, sp.BackendConfig.Name)
	klog.V(6).Infof("Backend config specified for service port %s: %s", svcPortKey, beConfig)

	if sp.BackendConfig.Spec.Cdn != nil && sp.BackendConfig.Spec.Cdn.Enabled {
		klog.V(6).Infof("Cloud CDN is enabled for service port %s", svcPortKey)
		features = append(features, cloudCDN)
	}
	if sp.BackendConfig.Spec.Iap != nil && sp.BackendConfig.Spec.Iap.Enabled {
		klog.V(6).Infof("Cloud IAP is enabled for service port %s", svcPortKey)
		features = append(features, cloudIAP)
	}
	// Possible list of Affinity types:
	// NONE, CLIENT_IP, GENERATED_COOKIE, CLIENT_IP_PROTO, or CLIENT_IP_PORT_PROTO.
	if sp.BackendConfig.Spec.SessionAffinity != nil {
		affinityType := sp.BackendConfig.Spec.SessionAffinity.AffinityType
		switch affinityType {
		case "GENERATED_COOKIE":
			features = append(features, cookieAffinity)
		case "CLIENT_IP", "CLIENT_IP_PROTO", "CLIENT_IP_PORT_PROTO":
			features = append(features, clientIPAffinity)
		}
		klog.V(6).Infof("Session affinity %s is configured for service port %s", affinityType, svcPortKey)
	}
	if sp.BackendConfig.Spec.SecurityPolicy != nil {
		klog.V(6).Infof("Security policy %s is configured for service port %s", sp.BackendConfig.Spec.SecurityPolicy, svcPortKey)
		features = append(features, cloudArmor)
	}
	if sp.BackendConfig.Spec.TimeoutSec != nil {
		klog.V(6).Infof("Backend timeout(%v secs) is configured for service port %s", sp.BackendConfig.Spec.TimeoutSec, svcPortKey)
		features = append(features, backendTimeout)
	}
	if sp.BackendConfig.Spec.ConnectionDraining != nil {
		klog.V(6).Infof("Backend connection draining(%v secs) is configured for service port %s", sp.BackendConfig.Spec.ConnectionDraining.DrainingTimeoutSec, svcPortKey)
		features = append(features, backendConnectionDraining)
	}
	if sp.BackendConfig.Spec.CustomRequestHeaders != nil {
		klog.V(6).Infof("Custom request headers configured for service port %s: %v", svcPortKey, sp.BackendConfig.Spec.CustomRequestHeaders.Headers)
		features = append(features, customRequestHeaders)
	}
	klog.V(4).Infof("Features for Service port %s: %v", svcPortKey, features)
	return features
}
