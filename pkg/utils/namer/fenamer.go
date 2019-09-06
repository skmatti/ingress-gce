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
	"fmt"
	"strings"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/klog"
)

const (
	LegacyNamingScheme          = Scheme("v1")
	V2NamingScheme              = Scheme("v2")
	schemaVersionV2             = "2"
	maxNameLength               = 63
	forwardingRulePrefixV2      = "fw"
	httpsForwardingRulePrefixV2 = "fs"
	targetHTTPProxyPrefixV2     = "tp"
	targetHTTPSProxyPrefixV2    = "ts"
)

type Scheme string

// LegacyIngressFrontendNamer implements IngressFrontendNamer. This is a wrapper on top of namer.Namer.
type LegacyIngressFrontendNamer struct {
	ing    *v1beta1.Ingress
	namer  *Namer
	lbName string
}

func NewLegacyIngressFrontendNamer(ing *v1beta1.Ingress, namer *Namer) IngressFrontendNamer {
	ingFeNamer := &LegacyIngressFrontendNamer{ing: ing, namer: namer}
	ingFeNamer.lbName = namer.LoadBalancer(IngressKeyFunc(ing))
	return ingFeNamer
}

func (ln *LegacyIngressFrontendNamer) ForwardingRule(protocol NamerProtocol) string {
	return ln.namer.ForwardingRule(ln.lbName, protocol)
}

func (ln *LegacyIngressFrontendNamer) TargetProxy(protocol NamerProtocol) string {
	return ln.namer.TargetProxy(ln.lbName, protocol)
}

func (ln *LegacyIngressFrontendNamer) UrlMap() string {
	return ln.namer.UrlMap(ln.lbName)
}

func (ln *LegacyIngressFrontendNamer) SSLCertName(secretHash string) string {
	return ln.namer.SSLCertName(ln.lbName, secretHash)
}

func (ln *LegacyIngressFrontendNamer) IsCertUsedForLB(certName string) bool {
	return ln.namer.IsCertUsedForLB(ln.lbName, certName)
}

func (ln *LegacyIngressFrontendNamer) IsLegacySSLCert(certName string) bool {
	return ln.namer.IsLegacySSLCert(ln.lbName, certName)
}

func (ln *LegacyIngressFrontendNamer) GetLbName() string {
	return ln.lbName
}

// V2IngressFrontendNamer implements IngresFrontendNamer.
type V2IngressFrontendNamer struct {
	ing      *v1beta1.Ingress
	oldnamer *Namer
	lbName   string
	// maximum combined length of namespace and name.
	maxLength int
}

func NewV2IngressFrontendNamer(ing *v1beta1.Ingress, oldnamer *Namer) IngressFrontendNamer {
	v2IngFeNamer := &V2IngressFrontendNamer{ing: ing, oldnamer: oldnamer}
	v2IngFeNamer.maxLength = v2IngFeNamer.getMaxLength()
	v2IngFeNamer.setLbName()
	return v2IngFeNamer
}

// ForwardingRule returns the name of forwarding rule based on given protocol.
func (vn *V2IngressFrontendNamer) ForwardingRule(protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, forwardingRulePrefixV2, vn.lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, httpsForwardingRulePrefixV2, vn.lbName)
	default:
		klog.Fatalf("invalid ForwardingRule protocol: %q", protocol)
		return "invalid"
	}
}

// TargetProxy returns the name of target proxy based on given protocol.
func (vn *V2IngressFrontendNamer) TargetProxy(protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, targetHTTPProxyPrefixV2, vn.lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, targetHTTPSProxyPrefixV2, vn.lbName)
	default:
		klog.Fatalf("invalid TargetProxy protocol: %q", protocol)
		return "invalid"
	}
}

// UrlMap returns the name of URL map.
func (vn *V2IngressFrontendNamer) UrlMap() string {
	return fmt.Sprintf("%s%s-%s", vn.oldnamer.prefix, schemaVersionV2, vn.lbName)
}

// SSLCertName returns the name of the certificate.
func (vn *V2IngressFrontendNamer) SSLCertName(secretHash string) string {
	return fmt.Sprintf("%s%s-%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, vn.oldnamer.UID(), vn.lbNameToHash(), secretHash)
}

// IsCertUsedForLB returns true if the certName belongs to this cluster's ingress.
// It checks that the hashed lbName exists.
func (vn *V2IngressFrontendNamer) IsCertUsedForLB(certName string) bool {
	prefix := fmt.Sprintf("%s%s-%s-%s", vn.oldnamer.prefix, schemaVersionV2, vn.oldnamer.UID(), vn.lbNameToHash())
	return strings.HasPrefix(certName, prefix)
}

// IsLegacySSLCert returns true if certName is this Ingress managed name following the older naming convention.
func (vn *V2IngressFrontendNamer) IsLegacySSLCert(certName string) bool {
	return vn.oldnamer.IsLegacySSLCert(vn.GetLbName(), certName)
}

// setLbName sets loadbalancer name.
func (vn *V2IngressFrontendNamer) setLbName() {
	truncFields := TrimFieldsEvenly(vn.maxLength, vn.ing.Namespace, vn.ing.Name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	suffix := vn.suffix(vn.oldnamer.UID(), vn.ing.Namespace, vn.ing.Name)
	vn.lbName = fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.shortUID(), truncNamespace, truncName, suffix)
}

// GetLbName returns loadbalancer name.
func (vn *V2IngressFrontendNamer) GetLbName() string {
	return vn.lbName
}

// suffix returns hash code with 8 characters.
func (vn *V2IngressFrontendNamer) suffix(uid, namespace, name string) string {
	lbString := strings.Join([]string{uid, namespace, name}, ";")
	return Hash(lbString, 8)
}

// lbNameToHash returns hash code of lbName with 16 characters.
func (vn *V2IngressFrontendNamer) lbNameToHash() string {
	return Hash(vn.lbName, 16)
}

// getMaxLength returns the maximum combined length of namespace and name.
func (vn *V2IngressFrontendNamer) getMaxLength() int {
	// k8s1-4, dashes-5, resource prefix-2, suffix hash-8
	return maxNameLength - 19 - len(vn.oldnamer.shortUID())
}

type IngressFrontendNamerFactoryImpl struct {
	namer *Namer
}

//NewIngressFrontendNamerFactoryImpl implements IngressFrontendNamrFactory.
func NewIngressFrontendNamerFactoryImpl(namer *Namer) IngressFrontendNamerFactory {
	return &IngressFrontendNamerFactoryImpl{namer: namer}
}

//CreateIngressFrontendNamer returns IngressFrontendNamer for a given naming scheme and Ingress.
func (rn *IngressFrontendNamerFactoryImpl) CreateIngressFrontendNamer(scheme Scheme, ing *v1beta1.Ingress) IngressFrontendNamer {
	switch scheme {
	case LegacyNamingScheme:
		return NewLegacyIngressFrontendNamer(ing, rn.namer)
	case V2NamingScheme:
		return NewV2IngressFrontendNamer(ing, rn.namer)
	default:
		return NewLegacyIngressFrontendNamer(ing, rn.namer)
	}
}
