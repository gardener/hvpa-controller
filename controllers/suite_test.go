/*
Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.

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

package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	hvpav1alpha1 "github.com/gardener/hvpa-controller/api/v1alpha1"
	mockclient "github.com/gardener/hvpa-controller/mock/controller-runtime/client"
	"github.com/golang/mock/gomock"
	gomegatypes "github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	Expect(hvpav1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	// +kubebuilder:scaffold:scheme
})

/* This hvpa results in following effective buckets after taking ScalingIntervalsOverlap into account:
 * {map[cpu:{10 1000} memory:{50M 2G} replicas:{1}]}
 * {map[cpu:{400 5300} memory:{990M 8.2425G} replicas:{2}]}
 * {map[cpu:{2827 10200} memory:{5.485G 15.495G} replicas:{3}]}
 * {map[cpu:{6120 15100} memory:{11.61125G 22.7475G} replicas:{4}]}
 * {map[cpu:{9664 20000} memory:{18.188G 30G} replicas:{5}]}
 * {map[cpu:{13333 30000} memory:{24.99 40G} replicas:{6}]}
 */
func newHvpa(name, target, labelVal string, minChange hvpav1alpha1.ScaleParams) *hvpav1alpha1.Hvpa {
	updateMode := hvpav1alpha1.UpdateModeAuto

	instance := &hvpav1alpha1.Hvpa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"hpa-controller": "hvpa",
			},
			UID: types.UID("1234567890"),
		},
		Spec: hvpav1alpha1.HvpaSpec{
			Replicas: int32Ptr(1),
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       target,
				APIVersion: "apps/v1",
			},
			Hpa: hvpav1alpha1.HpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"hpaKey": labelVal,
					},
				},
				Deploy: true,
				Template: hvpav1alpha1.HpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"hpaKey": labelVal,
						},
					},
					Spec: hvpav1alpha1.HpaTemplateSpec{
						MinReplicas: int32Ptr(1),
						MaxReplicas: 3,
						Metrics: []autoscaling.MetricSpec{
							{
								Type: autoscaling.ResourceMetricSourceType,
								Resource: &autoscaling.ResourceMetricSource{
									Name:                     v1.ResourceCPU,
									TargetAverageUtilization: int32Ptr(70),
								},
							},
						},
					},
				},
			},
			Vpa: hvpav1alpha1.VpaSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vpaKey": labelVal,
					},
				},
				Deploy: true,
				Template: hvpav1alpha1.VpaTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"vpaKey": labelVal,
						},
					},
					Spec: hvpav1alpha1.VpaTemplateSpec{
						ResourcePolicy: &vpa_api.PodResourcePolicy{
							ContainerPolicies: []vpa_api.ContainerResourcePolicy{
								{
									ContainerName: target,
									MinAllowed: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("200M"),
									},
									MaxAllowed: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("40"),
										v1.ResourceMemory: resource.MustParse("100G"),
									},
								},
							},
						},
					},
				},
			},
			ScaleUp: hvpav1alpha1.ScaleType{
				UpdatePolicy: hvpav1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
				StabilizationDuration: stringPtr("3m"),
				MinChange:             minChange,
			},
			ScaleDown: hvpav1alpha1.ScaleType{
				UpdatePolicy: hvpav1alpha1.UpdatePolicy{
					UpdateMode: &updateMode,
				},
				StabilizationDuration: stringPtr("3m"),
				MinChange:             minChange,
			},
			ScaleIntervals: []hvpav1alpha1.ScaleIntervals{
				{
					MaxCPU:      resourcePtr("1"),
					MaxMemory:   resourcePtr("2G"),
					MaxReplicas: 1,
				},
				{
					MaxCPU:      resourcePtr("20"),
					MaxMemory:   resourcePtr("30G"),
					MaxReplicas: 5,
				},
				{
					MaxCPU:      resourcePtr("30"),
					MaxMemory:   resourcePtr("40G"),
					MaxReplicas: 6,
				},
			},
			ScalingIntervalsOverlap: hvpav1alpha1.ResourceChangeParams{
				v1.ResourceCPU: hvpav1alpha1.ChangeParams{
					Percentage: int32Ptr(20),
				},
				v1.ResourceMemory: hvpav1alpha1.ChangeParams{
					Value: stringPtr("10M"),
				},
			},
		},
	}

	return instance
}

/* These scaleIntervals results in following effective buckets:
 * {1 map[cpu:100 memory:200000000000] map[cpu:400 memory:1000000000000]}
 * {2 map[cpu:160 memory:490000000000] map[cpu:600 memory:1500000000000]}
 * {3 map[cpu:320 memory:990000000000] map[cpu:800 memory:2000000000000]}
 * {4 map[cpu:480 memory:1490000000000] map[cpu:1000 memory:2750000000000]}
 * {5 map[cpu:640 memory:2190000000000] map[cpu:1500 memory:4000000000000]}
 * {6 map[cpu:1000 memory:3323333333333] map[cpu:1750 memory:6000000000000]}
 */
func newScaleInterval() []hvpav1alpha1.ScaleIntervals {
	return []hvpav1alpha1.ScaleIntervals{
		{
			MaxCPU:      resourcePtr("0.4"),
			MaxMemory:   resourcePtr("1G"),
			MaxReplicas: 1,
		},
		{
			MaxCPU:      resourcePtr("0.6"),
			MaxMemory:   resourcePtr("1.5G"),
			MaxReplicas: 2,
		},
		{
			MaxCPU:      resourcePtr("0.8"),
			MaxMemory:   resourcePtr("2G"),
			MaxReplicas: 3,
		},
		{
			MaxCPU:      resourcePtr("1"),
			MaxMemory:   resourcePtr("2.75G"),
			MaxReplicas: 4,
		},
		{
			MaxCPU:      resourcePtr("1.5"),
			MaxMemory:   resourcePtr("4G"),
			MaxReplicas: 5,
		},
		{
			MaxCPU:      resourcePtr("1.75"),
			MaxMemory:   resourcePtr("6G"),
			MaxReplicas: 6,
		},
	}
}

func newHpaStatus(currentReplicas, desiredReplicas int32, conditions []autoscaling.HorizontalPodAutoscalerCondition) *autoscaling.HorizontalPodAutoscalerStatus {
	return &autoscaling.HorizontalPodAutoscalerStatus{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
		Conditions:      conditions,
	}
}

func newVpaStatus(containerName, mem, cpu string) *vpa_api.VerticalPodAutoscalerStatus {
	return &vpa_api.VerticalPodAutoscalerStatus{
		Recommendation: &vpa_api.RecommendedPodResources{
			ContainerRecommendations: []vpa_api.RecommendedContainerResources{
				{
					ContainerName: containerName,
					Target: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse(cpu),
						v1.ResourceMemory: resource.MustParse(mem),
					},
				},
			},
		},
		Conditions: []vpa_api.VerticalPodAutoscalerCondition{
			{
				Status: "True",
				Type:   vpa_api.RecommendationProvided,
			},
			{
				Status: "False",
				Type:   vpa_api.ConfigUnsupported,
			},
		},
	}
}

func newTarget(name string, resources v1.ResourceRequirements, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": name,
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": name,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:      name,
							Image:     "k8s.gcr.io/pause-amd64:3.0",
							Resources: resources,
						},
					},
				},
			},
		},
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}

func int32Ptr(i int32) *int32 {
	return &i
}

func stringPtr(i string) *string {
	return &i
}

func resourcePtr(i string) *resource.Quantity {
	q := resource.MustParse(i)
	return &q
}

type nothingMatcher struct{}

func (m nothingMatcher) Matches(x interface{}) bool {
	return false
}

func (m nothingMatcher) String() string {
	return "is nothing"
}

func Nothing() gomock.Matcher {
	return nothingMatcher{}
}

type namespacedNameMatcher types.NamespacedName

func (m namespacedNameMatcher) Matches(x interface{}) bool {
	if k, ok := x.(types.NamespacedName); ok {
		return m.equal(types.NamespacedName(m), k)
	}
	if obj, ok := x.(client.Object); ok {
		return m.equal(types.NamespacedName(m), client.ObjectKeyFromObject(obj))
	}

	return false
}

func (m namespacedNameMatcher) equal(a, b types.NamespacedName) bool {
	return a.String() == b.String()
}

func (m namespacedNameMatcher) String() string {
	return fmt.Sprintf("matches key %q", types.NamespacedName(m).String())
}

func HasNamespacedName(key types.NamespacedName) gomock.Matcher {
	return namespacedNameMatcher(key)
}

func SameNamespacedNameAs(obj client.Object) gomock.Matcher {
	return HasNamespacedName(client.ObjectKeyFromObject(obj))
}

type groupKindMatcher struct {
	schema.GroupKind
	scheme *runtime.Scheme
}

func (m groupKindMatcher) Matches(x interface{}) bool {
	if obj, ok := x.(runtime.Object); ok {
		if gvk, err := apiutil.GVKForObject(obj, m.scheme); err != nil {
			return false
		} else {
			return m.GroupKind == gvk.GroupKind()
		}
	}

	return false
}

func (m groupKindMatcher) String() string {
	return fmt.Sprintf("is of GroupKind %q", m.GroupKind)
}

func OfGroupKind(gk schema.GroupKind, s *runtime.Scheme) gomock.Matcher {
	return groupKindMatcher{
		GroupKind: gk,
		scheme:    s,
	}
}

func SameGroupKindAs(obj client.Object, s *runtime.Scheme) gomock.Matcher {
	if gk, err := apiutil.GVKForObject(obj, s); err != nil {
		return Nothing()
	} else {
		return OfGroupKind(gk.GroupKind(), s)
	}
}

func mockGet(cl *mockclient.MockClient, src client.Object, errorFn func() error) *gomock.Call {
	return cl.EXPECT().Get(gomock.Any(), SameNamespacedNameAs(src), SameGroupKindAs(src, cl.Scheme())).DoAndReturn(
		func(_ context.Context, _ client.ObjectKey, target client.Object) error {
			if err := errorFn(); err != nil {
				return err
			}

			Expect(cl.Scheme().Convert(src, target, nil)).To(Succeed())
			return nil
		},
	)
}

func newNotFoundError(sch *runtime.Scheme, obj client.Object) error {
	gvk, err := apiutil.GVKForObject(obj, sch)
	if err != nil {
		return err
	}

	return apierrors.NewNotFound(
		schema.GroupResource{
			Group:    gvk.Group,
			Resource: strings.ToLower(gvk.Kind),
		},
		obj.GetName(),
	)
}

type quantityMatcher struct {
	resource.Quantity
}

func (m *quantityMatcher) Match(actual interface{}) (success bool, err error) {
	if q, ok := actual.(resource.Quantity); ok {
		return m.Quantity.Equal(q), nil
	}

	return false, fmt.Errorf("EqualQuantity matcher expects resource.Quantity. Got: %t", actual)
}

func (m *quantityMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to equal", m.Quantity)
}

func (m *quantityMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to equal", m.Quantity)
}

func (m *quantityMatcher) String() string {
	return fmt.Sprintf("resource.Quantity.MustParse(%q)", m.Quantity.String())
}

func EqualQuantity(q *resource.Quantity) gomegatypes.GomegaMatcher {
	if q == nil {
		return BeNil()
	}

	return &quantityMatcher{Quantity: q.DeepCopy()}
}

var nop = func() {}
