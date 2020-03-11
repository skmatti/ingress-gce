/*
Copyright 2018 The Kubernetes Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +k8s:openapi-gen=true
type BackendConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackendConfigSpec   `json:"spec,omitempty" protobuf:"bytes,1,opt,name=spec"`
	Status BackendConfigStatus `json:"status,omitempty" protobuf:"bytes,2,opt,name=status"`
}

// BackendConfigSpec is the spec for a BackendConfig resource
// +k8s:openapi-gen=true
type BackendConfigSpec struct {
	Iap                *IAPConfig                `json:"iap,omitempty" protobuf:"bytes,1,opt,name=iap"`
	Cdn                *CDNConfig                `json:"cdn,omitempty" protobuf:"bytes,2,opt,name=cdn"`
	SecurityPolicy     *SecurityPolicyConfig     `json:"securityPolicy,omitempty" protobuf:"bytes,3,opt,name=securityPolicy"`
	TimeoutSec         *int64                    `json:"timeoutSec,omitempty" protobuf:"bytes,4,opt,name=timeoutSec"`
	ConnectionDraining *ConnectionDrainingConfig `json:"connectionDraining,omitempty" protobuf:"bytes,5,opt,name=connectionDraining"`
	SessionAffinity    *SessionAffinityConfig    `json:"sessionAffinity,omitempty" protobuf:"bytes,6,opt,name=sessionAffinity"`
}

// BackendConfigStatus is the status for a BackendConfig resource
type BackendConfigStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackendConfigList is a list of BackendConfig resources
type BackendConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata" protobuf:"bytes,1,opt,name=metadata"`

	Items []BackendConfig `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// IAPConfig contains configuration for IAP-enabled backends.
// +k8s:openapi-gen=true
type IAPConfig struct {
	Enabled                bool                    `json:"enabled" protobuf:"bytes,1,opt,name=enabled"`
	OAuthClientCredentials *OAuthClientCredentials `json:"oauthclientCredentials" protobuf:"bytes,2,opt,name=oauthclientCredentials"`
}

// OAuthClientCredentials contains credentials for a single IAP-enabled backend.
// +k8s:openapi-gen=true
type OAuthClientCredentials struct {
	// The name of a k8s secret which stores the OAuth client id & secret.
	SecretName string `json:"secretName" protobuf:"bytes,1,opt,name=secretName"`
	// Direct reference to OAuth client id.
	ClientID string `json:"clientID,omitempty" protobuf:"bytes,2,opt,name=clientID"`
	// Direct reference to OAuth client secret.
	ClientSecret string `json:"clientSecret,omitempty" protobuf:"bytes,3,opt,name=clientSecret"`
}

// CDNConfig contains configuration for CDN-enabled backends.
// +k8s:openapi-gen=true
type CDNConfig struct {
	Enabled     bool            `json:"enabled" protobuf:"bytes,1,opt,name=enabled"`
	CachePolicy *CacheKeyPolicy `json:"cachePolicy,omitempty" protobuf:"bytes,2,opt,name=cachePolicy"`
}

// CacheKeyPolicy contains configuration for how requests to a CDN-enabled backend are cached.
// +k8s:openapi-gen=true
type CacheKeyPolicy struct {
	// If true, requests to different hosts will be cached separately.
	IncludeHost bool `json:"includeHost,omitempty" protobuf:"bytes,1,opt,name=includeHost"`
	// If true, http and https requests will be cached separately.
	IncludeProtocol bool `json:"includeProtocol,omitempty" protobuf:"bytes,2,opt,name=includeProtocol"`
	// If true, query string parameters are included in the cache key
	// according to QueryStringBlacklist and QueryStringWhitelist.
	// If neither is set, the entire query string is included and if false
	// the entire query string is excluded.
	IncludeQueryString bool `json:"includeQueryString,omitempty" protobuf:"bytes,3,opt,name=includeQueryString"`
	// Names of query strint parameters to exclude from cache keys. All other
	// parameters are included. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringBlacklist []string `json:"queryStringBlacklist,omitempty" protobuf:"bytes,4,rep,name=queryStringBlacklist"`
	// Names of query string parameters to include in cache keys. All other
	// parameters are excluded. Either specify QueryStringBlacklist or
	// QueryStringWhitelist, but not both.
	QueryStringWhitelist []string `json:"queryStringWhitelist,omitempty" protobuf:"bytes,5,rep,name=queryStringWhitelist"`
}

// SecurityPolicyConfig contains configuration for CloudArmor-enabled backends.
type SecurityPolicyConfig struct {
	// Name of the security policy that should be associated.
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
}

// ConnectionDrainingConfig contains configuration for connection draining.
// For now the draining timeout. May manage more settings in the future.
// +k8s:openapi-gen=true
type ConnectionDrainingConfig struct {
	// Draining timeout in seconds.
	DrainingTimeoutSec int64 `json:"drainingTimeoutSec,omitempty" protobuf:"bytes,1,opt,name=drainingTimeoutSec"`
}

// SessionAffinityConfig contains configuration for stickyness parameters.
// +k8s:openapi-gen=true
type SessionAffinityConfig struct {
	AffinityType         string `json:"affinityType,omitempty" protobuf:"bytes,1,opt,name=affinityType"`
	AffinityCookieTtlSec *int64 `json:"affinityCookieTtlSec,omitempty" protobuf:"bytes,2,opt,name=affinityCookieTtlSec"`
}
