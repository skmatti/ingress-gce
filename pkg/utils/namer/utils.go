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
	"strconv"
	"strings"

	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
)

const ClusterUIDLength = 8

// lookup table to maintain entropy when converting bytes to string.
var table []string

func init() {
	for i := 0; i < 10; i++ {
		table = append(table, strconv.Itoa(i))
	}
	for i := 0; i < 26; i++ {
		table = append(table, string('a'+rune(i)))
	}
}

func IngressKeyFunc(ing *v1beta1.Ingress) string {
	if ing == nil {
		return ""
	}
	return types.NamespacedName{Namespace: ing.Namespace, Name: ing.Name}.String()
}

// TrimFieldsEvenly trims the fields evenly and keeps the total length
// <= max. Truncation is spread in ratio with their original length,
// meaning smaller fields will be truncated less than longer ones.
func TrimFieldsEvenly(max int, fields ...string) []string {
	if max <= 0 {
		return fields
	}
	total := 0
	for _, s := range fields {
		total += len(s)
	}
	if total <= max {
		return fields
	}
	// Distribute truncation evenly among the fields.
	excess := total - max
	remaining := max
	var lengths []int
	for _, s := range fields {
		// Scale truncation to shorten longer fields more than ones that are already short.
		l := len(s) - len(s)*excess/total - 1
		lengths = append(lengths, l)
		remaining -= l
	}
	// Add fractional space that was rounded down.
	for i := 0; i < remaining; i++ {
		lengths[i]++
	}

	var ret []string
	for i, l := range lengths {
		ret = append(ret, fields[i][:l])
	}

	return ret
}

// IsV2UrlMap returns true if the name is a URL Map created using new name scheme and owned by this cluster.
// It checks that the UID is present and a substring of the
// cluster uid, since the new naming schema truncates it to 8 characters.
// URL map owned by another cluster may be considered as an URL map owned by this cluster if
// 8 character cluster prefix matches. This should be fine as URL map is deleted only if
// the full name matches with an URL map of any Ingress from this cluster.
func IsV2UrlMap(name string, n *Namer) bool {
	return strings.HasPrefix(name, fmt.Sprintf("%s%s-%s", n.prefix, schemaVersionV2, n.shortUID()))
}

func ParseV2ClusterUID(umName string) string {
	parts := strings.Split(umName, "-")
	if len(parts) < 3 || !strings.HasSuffix(parts[0], schemaVersionV2) {
		klog.Fatalf("invalid URL map name: %s", umName)
		return ""
	}
	return parts[1]
}

// Hash creates a content hash string of length n of s utilizing sha256.
func Hash(s string, n int) string {
	var h string
	bytes := sha256.Sum256(([]byte)(s))
	for i := 0; i < n && i < len(bytes); i++ {
		idx := int(bytes[i]) % len(table)
		h += table[idx]
	}
	return h
}
