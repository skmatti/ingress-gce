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

package loadbalancers

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
)

const (
	// Every target https proxy accepts upto 10 ssl certificates.
	TargetProxyCertLimit = 10
)

// checkProxy ensures the correct TargetHttpProxy for a loadbalancer
func (l *L7) checkProxy() (err error) {
	key, err := l.CreateKey(l.um.Name)
	if err != nil {
		return err
	}
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "urlMaps", Key: key}
	urlMapLink := resourceID.ResourcePath()
	proxyName := l.feNamer.TargetProxy(namer.HTTPProtocol)
	key, err = l.CreateKey(proxyName)
	if err != nil {
		return err
	}
	version := l.Versions().TargetHttpProxy
	proxy, _ := composite.GetTargetHttpProxy(l.cloud, key, version)
	if proxy == nil {
		klog.V(3).Infof("Creating new http proxy for urlmap %v", l.um.Name)
		description, err := l.description()
		if err != nil {
			return err
		}
		newProxy := &composite.TargetHttpProxy{
			Name:        proxyName,
			UrlMap:      urlMapLink,
			Description: description,
			Version:     version,
		}
		key, err := l.CreateKey(newProxy.Name)
		if err != nil {
			return err
		}
		if err = composite.CreateTargetHttpProxy(l.cloud, key, newProxy); err != nil {
			return err
		}
		key, err = l.CreateKey(proxyName)
		if err != nil {
			return err
		}
		proxy, err = composite.GetTargetHttpProxy(l.cloud, key, version)
		if err != nil {
			return err
		}
		l.tp = proxy
		return nil
	}
	if !utils.EqualResourcePaths(proxy.UrlMap, urlMapLink) {
		klog.V(3).Infof("Proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, urlMapLink, proxy.UrlMap)
		key, err := l.CreateKey(proxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetUrlMapForTargetHttpProxy(l.cloud, key, proxy, urlMapLink); err != nil {
			return err
		}
	}
	l.tp = proxy
	return nil
}

func (l *L7) checkHttpsProxy() (err error) {
	if len(l.sslCerts) == 0 {
		klog.V(3).Infof("No SSL certificates for %q, will not create HTTPS proxy.", l.GetName())
		return nil
	}

	key, err := l.CreateKey(l.um.Name)
	if err != nil {
		return err
	}
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "urlMaps", Key: key}
	urlMapLink := resourceID.ResourcePath()
	proxyName := l.feNamer.TargetProxy(namer.HTTPSProtocol)
	key, err = l.CreateKey(proxyName)
	if err != nil {
		return err
	}
	version := l.Versions().TargetHttpProxy
	proxy, _ := composite.GetTargetHttpsProxy(l.cloud, key, version)
	description, err := l.description()
	if err != nil {
		return err
	}
	if proxy == nil {
		klog.V(3).Infof("Creating new https proxy for urlmap %q", l.um.Name)
		newProxy := &composite.TargetHttpsProxy{
			Name:        proxyName,
			UrlMap:      urlMapLink,
			Description: description,
			Version:     version,
		}

		for _, c := range l.sslCerts {
			newProxy.SslCertificates = append(newProxy.SslCertificates, c.SelfLink)
		}

		key, err := l.CreateKey(newProxy.Name)
		if err != nil {
			return err
		}
		if err = composite.CreateTargetHttpsProxy(l.cloud, key, newProxy); err != nil {
			return err
		}

		key, err = l.CreateKey(proxyName)
		if err != nil {
			return err
		}
		proxy, err = composite.GetTargetHttpsProxy(l.cloud, key, version)
		if err != nil {
			return err
		}

		l.tps = proxy
		return nil
	}
	if !utils.EqualResourcePaths(proxy.UrlMap, urlMapLink) {
		klog.V(3).Infof("Https proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, urlMapLink, proxy.UrlMap)
		key, err := l.CreateKey(proxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetUrlMapForTargetHttpsProxy(l.cloud, key, proxy, urlMapLink); err != nil {
			return err
		}
	}

	if !l.compareCerts(proxy.SslCertificates) {
		klog.V(3).Infof("Https proxy %q has the wrong ssl certs, setting %v overwriting %v",
			proxy.Name, toCertNames(l.sslCerts), proxy.SslCertificates)
		var sslCertURLs []string
		for _, cert := range l.sslCerts {
			sslCertURLs = append(sslCertURLs, cert.SelfLink)
		}
		key, err := l.CreateKey(proxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetSslCertificateForTargetHttpsProxy(l.cloud, key, proxy, sslCertURLs); err != nil {
			return err
		}

	}
	l.tps = proxy
	return nil
}

func (l *L7) getSslCertLinkInUse() ([]string, error) {
	proxyName := l.feNamer.TargetProxy(namer.HTTPSProtocol)
	key, err := l.CreateKey(proxyName)
	if err != nil {
		return nil, err
	}
	proxy, err := composite.GetTargetHttpsProxy(l.cloud, key, l.Versions().TargetHttpsProxy)
	if err != nil {
		return nil, err
	}

	return proxy.SslCertificates, nil
}
