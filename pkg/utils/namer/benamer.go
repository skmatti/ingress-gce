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

// BackendNamerImpl implements BackendNamer. Naming policy for gce backend services and instance groups.
type BackendNamerImpl struct {
	namer *Namer
}

func NewBackendNamer(namer *Namer) BackendNamer {
	return BackendNamerImpl{namer: namer}
}

// IGBackend returns backend name for given node port.
func (bn BackendNamerImpl) IGBackend(nodePort int64) string {
	return bn.namer.IGBackend(nodePort)
}

// NEG returns gce neg name based on the service namespace, name
// and target port.
func (bn BackendNamerImpl) NEG(namespace, name string, port int32) string {
	return bn.namer.NEG(namespace, name, port)
}

// InstanceGroup returns instance group name.
func (bn BackendNamerImpl) InstanceGroup() string {
	return bn.namer.InstanceGroup()
}
