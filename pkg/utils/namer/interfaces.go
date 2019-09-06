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
	"k8s.io/api/networking/v1beta1"
)

// IngressFrontendNamer is an interface to name GCE frontend resources.
type IngressFrontendNamer interface {
	ForwardingRule(protocol NamerProtocol) string
	TargetProxy(protocol NamerProtocol) string
	UrlMap() string
	SSLCertName(secretHash string) string
	IsCertUsedForLB(resourceName string) bool
	IsLegacySSLCert(resourceName string) bool
	GetLbName() string
}

// IngressFrontendNamer is an interface to create a front namer for an Ingress.
type IngressFrontendNamerFactory interface {
	CreateIngressFrontendNamer(scheme Scheme, ing *v1beta1.Ingress) IngressFrontendNamer
}

// BackendNamer is an interface to name GCE backend resources.
type BackendNamer interface {
	IGBackend(nodePort int64) string
	NEG(namesapce, name string, Port int32) string
	InstanceGroup() string
}
