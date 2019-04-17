/*
Copyright 2019 Gaurav Gupta.

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

// NOTE: Boilerplate only.  Ignore this file.

// Package v1alpha1 contains API Schema definitions for the autoscaling v1alpha1 API group
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen=package,register
// +k8s:conversion-gen=k8s.io/autoscaler/hvpa-controller/pkg/apis/autoscaling
// +k8s:defaulter-gen=TypeMeta
// +groupName=autoscaling.k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
)

/*var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "autoscaling.k8s.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme is required by pkg/client/...
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource is required by pkg/client/listers/...
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	// SchemeBuilder points to a list of functions added to Scheme.
	SchemeBuilder   runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder
	// AddToSchemeVpa applies all the stored functions to the scheme.
	AddToScheme = localSchemeBuilder.AddToScheme
)*/

var (
	// SchemeBuilder used to register the Machine resource.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	localSchemeBuilder = &SchemeBuilder

	// AddToScheme is a pointer to SchemeBuilder.AddToScheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

func init() {
	// We only register manually written functions here. The registration of the
	// generated functions takes place in the generated files. The separation
	// makes the code compile even when the generated files are missing.
	localSchemeBuilder.Register(addKnownTypes)
}

// GroupName is the group name use in this package
const GroupName = "autoscaling.k8s.io"

// SchemeGroupVersionVpa is group version used to register these objects
var SchemeGroupVersionVpa = schema.GroupVersion{Group: GroupName, Version: "v1beta2"}

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Hvpa{},
		&HvpaList{},
	)
	scheme.AddKnownTypes(SchemeGroupVersionVpa,
		&vpa_api.VerticalPodAutoscaler{},
		&vpa_api.VerticalPodAutoscalerList{},
	)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Group: "autoscaling.k8s.io", Version: "v1alpha1"})
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Group: "autoscaling.k8s.io", Version: "v1beta2"})
	return nil
}
