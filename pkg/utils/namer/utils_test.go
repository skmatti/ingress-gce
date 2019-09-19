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

import "testing"

func TestTrimFieldsEvenly(t *testing.T) {
	t.Parallel()
	longString := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc   string
		fields []string
		expect []string
		max    int
	}{
		{
			"no change",
			[]string{longString},
			[]string{longString},
			100,
		},
		{
			"equal to max and no change",
			[]string{longString, longString},
			[]string{longString, longString},
			100,
		},
		{
			"equally trimmed to half",
			[]string{longString, longString},
			[]string{longString[:25], longString[:25]},
			50,
		},
		{
			"trimmed to only 10",
			[]string{longString, longString, longString},
			[]string{longString[:4], longString[:3], longString[:3]},
			10,
		},
		{
			"trimmed to only 3",
			[]string{longString, longString, longString},
			[]string{longString[:1], longString[:1], longString[:1]},
			3,
		},
		{
			"one long field with one short field",
			[]string{longString, longString[:1]},
			[]string{longString[:1], ""},
			1,
		},
		{
			"one long field with one short field and trimmed to 5",
			[]string{longString, longString[:1]},
			[]string{longString[:5], ""},
			5,
		},
	}

	for _, tc := range testCases {
		res := TrimFieldsEvenly(tc.max, tc.fields...)
		if len(res) != len(tc.expect) {
			t.Fatalf("%s: expect length == %d, got %d", tc.desc, len(tc.expect), len(res))
		}

		totalLen := 0
		for i := range res {
			totalLen += len(res[i])
			if res[i] != tc.expect[i] {
				t.Errorf("%s: the %d field is want to be %q, but got %q", tc.desc, i, tc.expect[i], res[i])
			}
		}

		if tc.max < totalLen {
			t.Errorf("%s: expect totalLen to be less than %d, but got %d", tc.desc, tc.max, totalLen)
		}
	}
}

func TestIsV2UrlMap(t *testing.T) {
	t.Parallel()
	defaultNamer := NewNamer("cluster1", "")
	testCases := []struct {
		desc string
		name string
		want bool
	}{
		{"old namer", "k8s-um-namespace-name--cluster1", false},
		{"old namer, with truncated name", "k8s-um-012345678901234567890123456789-0123456789012345678901230", false},
		{"new namer", "k8s2-cluster1-namespace-name-735cdc61", true},
		{"new namer, owned by cluster123", "k8s2-cluster1-namespace-name-1ebef71c", true},
		{"new namer, with truncated name", "k8s2-cluster1-01234567890123456789-0123456789012345-49aea33c", true},
	}

	for _, tc := range testCases {
		if got := IsV2UrlMap(tc.name, defaultNamer); got != tc.want {
			t.Errorf("IsV2UrlMap(%q, %v) = %t, want %t (%s)", tc.name, defaultNamer, got, tc.want, tc.desc)
		}
	}
}
