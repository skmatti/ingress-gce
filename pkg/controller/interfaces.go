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

package controller

// Syncer is an interface to sync GCP resources associated with an Ingress.
type Syncer interface {
	// Sync creates a full GCLB given some state related to an Ingress.
	Sync(state *syncState) error
	// GC cleans up GCLB resources for all Ingresses and can optionally
	// use some arbitrary to help with the process.
	// TODO(rramkumar): Do we need to rethink the strategy of GC'ing
	// all Ingresses at once?
	GC(state *gcState) error
}
