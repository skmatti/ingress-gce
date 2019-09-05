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
	"strings"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/klog"
)

const (
	LegacyNamingScheme = Scheme("v1")
	V2NamingScheme     = Scheme("v2")
	maxLength          = 36
)

type Scheme string

// IngressFrontendNamer is an interface to name GCE frontend resources.
type IngressFrontendNamer interface {
	ForwardingRule(protocol NamerProtocol) string
	TargetProxy(protocol NamerProtocol) string
	UrlMap() string
	SSLCertName(secretHash string) string
	IsCertUsedForLB(resourceName string) bool
	IsLegacySSLCert(resourceName string) bool
	GetBENamer() *Namer
	GetLbName() string
}

// IngressFrontendNamer is an interface to create a front namer for Ingress.
type IngressFrontendNamerFactory interface {
	CreateIngressFrontendNamer(scheme Scheme, ing *v1beta1.Ingress) IngressFrontendNamer
}

// LegacyIngressFrontendNamer implements IngressFrontendNamer. This is a wrapper on top of namer.Namer.
type LegacyIngressFrontendNamer struct {
	ing   *v1beta1.Ingress
	namer *Namer

	//optional
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

func (ln *LegacyIngressFrontendNamer) IsCertUsedForLB(resourceName string) bool {
	return ln.namer.IsCertUsedForLB(ln.lbName, resourceName)
}

func (ln *LegacyIngressFrontendNamer) IsLegacySSLCert(resourceName string) bool {
	return ln.namer.IsLegacySSLCert(ln.lbName, resourceName)
}

func (ln *LegacyIngressFrontendNamer) GetBENamer() *Namer {
	return ln.namer
}

func (ln *LegacyIngressFrontendNamer) GetLbName() string {
	return ln.lbName
}

// V2IngressFrontendNamer
type V2IngressFrontendNamer struct {
	ing      *v1beta1.Ingress
	oldnamer *Namer
}

func NewV2IngressFrontendNamer(ing *v1beta1.Ingress, oldnamer *Namer) IngressFrontendNamer {
	return &V2IngressFrontendNamer{ing: ing, oldnamer: oldnamer}
}

func (vn *V2IngressFrontendNamer) ForwardingRule(protocol NamerProtocol) string {
	lbName := vn.GetLbName()
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.prefix, forwardingRulePrefix, vn.shortUID(), lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.prefix, httpsForwardingRulePrefix, vn.shortUID(), lbName)
	default:
		klog.Fatalf("invalid ForwardingRule protocol: %q", protocol)
		return "invalid"
	}
}

func (vn *V2IngressFrontendNamer) TargetProxy(protocol NamerProtocol) string {
	lbName := vn.GetLbName()
	switch protocol {
	case HTTPProtocol:
		return fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.prefix, targetHTTPProxyPrefix, vn.shortUID(), lbName)
	case HTTPSProtocol:
		return fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.prefix, targetHTTPSProxyPrefix, vn.shortUID(), lbName)
	default:
		klog.Fatalf("invalid TargetProxy protocol: %q", protocol)
		return "invalid"
	}
}

func (vn *V2IngressFrontendNamer) UrlMap() string {
	return fmt.Sprintf("%s-%s-%s-%s", vn.oldnamer.prefix, urlMapPrefix, vn.shortUID(), vn.GetLbName())
}

func (vn *V2IngressFrontendNamer) SSLCertName(secretHash string) string {
	return ""
}

func (vn *V2IngressFrontendNamer) IsCertUsedForLB(resourceName string) bool {
	return false
}

func (vn *V2IngressFrontendNamer) IsLegacySSLCert(resourceName string) bool {
	return false
}

func (vn *V2IngressFrontendNamer) GetBENamer() *Namer {
	return vn.oldnamer
}

func (vn *V2IngressFrontendNamer) GetLbName() string {
	truncFields := TrimFieldsEvenly(maxLength, vn.ing.Namespace, vn.ing.Name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	suffix := vn.suffix(vn.ing.ClusterName, vn.ing.Namespace, vn.ing.Name)
	return fmt.Sprintf("%s-%s-%s", truncNamespace, truncName, suffix)
}

// shortUID returns UID/ cluster name truncated to 8 chars.
func (vn *V2IngressFrontendNamer) shortUID() string {
	if len(vn.ing.ClusterName) <= 8 {
		return vn.ing.ClusterName
	}
	return vn.ing.ClusterName[:8]
}

// suffix returns hash code with 8 characters
func (vn *V2IngressFrontendNamer) suffix(uid, namespace, name string) string {
	lbString := strings.Join([]string{uid, namespace, name}, ";")
	lbHash := fmt.Sprintf("%x", sha256.Sum256([]byte(lbString)))
	return lbHash[:8]
}

type IngressFrontendNamerFactoryImpl struct {
	namer *Namer
}

func NewIngressFrontendNamerFactoryImpl(namer *Namer) IngressFrontendNamerFactory {
	return &IngressFrontendNamerFactoryImpl{namer: namer}
}

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
