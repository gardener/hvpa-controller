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
	"errors"
	"time"

	hvpav1alpha2 "github.com/gardener/hvpa-controller/api/v1alpha2"
	mockclient "github.com/gardener/hvpa-controller/mock/controller-runtime/client"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("LabelSelectorAsSelector", func() {
	Describe("with nil selector", func() {
		It("should match nothing", func() {
			Expect(metav1.LabelSelectorAsSelector(nil)).To(Equal(labels.Nothing()))
		})
	})
})

var _ = Describe("BaseControllerRefManager", func() {
	var (
		sch                *runtime.Scheme
		controller         *hvpav1alpha2.Hvpa
		m                  *BaseControllerRefManager
		obj                *autoscaling.HorizontalPodAutoscaler
		matchFn            func(metav1.Object) bool
		adoptFn, releaseFn func(metav1.Object) error
		matchClaim         gomegatypes.GomegaMatcher
		matchErr           gomegatypes.GomegaMatcher

		describeControllerWithDeletionTimestamp = func() {
			Describe("when controller has deletion timestamp set", func() {
				BeforeEach(func() {
					m.Controller.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})

					matchClaim = BeFalse()
					matchErr = Succeed()
				})

				It("should not be claimed", nop)
			})
		}
	)

	BeforeEach(func() {
		sch = scheme.Scheme

		controller = &hvpav1alpha2.Hvpa{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("1234"),
			},
		}

		m = &BaseControllerRefManager{Controller: controller}

		obj = &autoscaling.HorizontalPodAutoscaler{}

		matchFn = nil
		adoptFn = nil
		releaseFn = nil

		matchClaim = nil
		matchErr = nil
	})

	JustBeforeEach(func() {
		controllerBeforeClaim := controller.DeepCopy()
		objBeforeClaim := obj.DeepCopy()

		claim, err := m.ClaimObject(obj, matchFn, adoptFn, releaseFn)

		Expect(err).To(matchErr)
		Expect(claim).To(matchClaim)

		// ClaimObject itself should not modify the controller and the object.
		Expect(controller).To(Equal(controllerBeforeClaim))
		Expect(obj).To(Equal(objBeforeClaim))
	})

	Describe("when object already has a controller reference", func() {
		Describe("of a different controller", func() {
			BeforeEach(func() {
				nc := controller.DeepCopy()
				nc.SetUID(types.UID("9876"))

				Expect(controllerutil.SetControllerReference(nc, obj, sch)).To(Succeed())

				matchClaim = BeFalse()
				matchErr = Succeed()
			})

			It("should not be claimed", nop)
		})

		Describe("of the same controller", func() {
			BeforeEach(func() {
				Expect(controllerutil.SetControllerReference(controller, obj, sch)).To(Succeed())
			})

			Describe("and matches criteria", func() {
				BeforeEach(func() {
					matchFn = func(_ metav1.Object) bool { return true }

					matchClaim = BeTrue()
					matchErr = Succeed()
				})

				It("should be claimed unmodified", nop)
			})

			Describe("and does not match criteria", func() {
				BeforeEach(func() {
					matchFn = func(_ metav1.Object) bool { return false }
				})

				describeControllerWithDeletionTimestamp()

				Describe("release call", func() {
					var releaseErr error

					BeforeEach(func() {
						releaseFn = func(_ metav1.Object) error { return releaseErr }

						matchClaim = BeFalse()
					})

					Describe("fails", func() {
						Describe("with not found error", func() {
							BeforeEach(func() {
								releaseErr = newNotFoundError(sch, obj)

								matchErr = Succeed()
							})

							It("should neither be claimed not released", nop)
						})

						Describe("with any other error", func() {
							BeforeEach(func() {
								releaseErr = errors.New("err")

								matchErr = MatchError(releaseErr)
							})

							It("should neither be claimed nor released", nop)
						})
					})

					Describe("succeeds", func() {
						BeforeEach(func() {
							releaseErr = nil

							matchErr = Succeed()
						})

						It("should be released and not claimed", nop)
					})
				})
			})
		})
	})

	Describe("when object does not have controller reference", func() {
		describeControllerWithDeletionTimestamp()

		Describe("and does not match criteria", func() {
			BeforeEach(func() {
				matchFn = func(_ metav1.Object) bool { return false }

				matchClaim = BeFalse()
				matchErr = Succeed()
			})

			It("should not be claimed", nop)
		})

		Describe("and matches criteria", func() {
			BeforeEach(func() {
				matchFn = func(_ metav1.Object) bool { return true }
			})

			Describe("adopt call", func() {
				var adoptErr error

				BeforeEach(func() {
					adoptFn = func(_ metav1.Object) error { return adoptErr }
				})

				Describe("fails", func() {
					BeforeEach(func() {
						matchClaim = BeFalse()
					})

					Describe("with not found error", func() {
						BeforeEach(func() {
							adoptErr = newNotFoundError(sch, obj)

							matchErr = Succeed()
						})

						It("should not be claimed", nop)
					})

					Describe("with any other error", func() {
						BeforeEach(func() {
							adoptErr = errors.New("err")

							matchErr = MatchError(adoptErr)
						})

						It("should not be claimed", nop)
					})
				})

				Describe("succeeds", func() {
					BeforeEach(func() {
						adoptErr = nil

						matchClaim = BeTrue()
						matchErr = Succeed()
					})

					It("should be claimed", nop)
				})
			})
		})
	})
})

var _ = Describe("HvpaControllerRefManager", func() {
	var (
		cl         *mockclient.MockClient
		controller *hvpav1alpha2.Hvpa
		cm         *HvpaControllerRefManager
	)

	BeforeEach(func() {
		cl = mockclient.NewMockClient(gomock.NewController(GinkgoT()))

		cl.EXPECT().Scheme().Return(scheme.Scheme).AnyTimes()

		controller = &hvpav1alpha2.Hvpa{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controller",
				Namespace: "default",
				UID:       types.UID("1234"),
			},
		}

		cm = NewHvpaControllerRefManager(
			&HvpaReconciler{
				Client: cl,
				Scheme: cl.Scheme(),
			},
			controller,
			func() labels.Selector {
				var req, err = labels.NewRequirement("matches", selection.Equals, []string{"true"})
				Expect(err).ToNot(HaveOccurred())

				return labels.NewSelector().Add(*req)
			}(),
			hvpav1alpha2.SchemeGroupVersionHvpa.WithKind("Hvpa"),
			func() error { return nil },
		)
	})

	Describe("ClaimHpas", func() {
		var (
			hpaList     *autoscaling.HorizontalPodAutoscalerList
			claimedHpas []*autoscaling.HorizontalPodAutoscaler
			matchErr    gomegatypes.GomegaMatcher
		)

		BeforeEach(func() {
			hpaList = &autoscaling.HorizontalPodAutoscalerList{
				Items: []autoscaling.HorizontalPodAutoscaler{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-yet-owned-but-to-be-adopted",
							Namespace: controller.Namespace,
							Labels: map[string]string{
								"matches": "true",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "already-owned-but-to-be-released",
							Namespace: controller.Namespace,
							Labels: map[string]string{
								"matches": "false",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion:         hvpav1alpha2.SchemeGroupVersionHvpa.String(),
									Kind:               "Hvpa",
									Name:               controller.GetName(),
									UID:                controller.GetUID(),
									BlockOwnerDeletion: pointer.BoolPtr(true),
									Controller:         pointer.BoolPtr(true),
								},
							},
						},
					},
				},
			}

			claimedHpas = nil
		})

		JustBeforeEach(func() {
			var hpas, err = cm.ClaimHpas(hpaList)

			Expect(err).To(matchErr)

			if claimedHpas == nil {
				Expect(hpas).To(BeEmpty())
			} else {
				Expect(hpas).To(Equal(claimedHpas))
			}
		})

		Describe("patch call", func() {
			var patchErr error

			BeforeEach(func() {
				cl.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{}), gomock.Any()).DoAndReturn(
					func(_ context.Context, src *autoscaling.HorizontalPodAutoscaler, _ client.Patch) error {
						if patchErr != nil {
							return patchErr
						}

						for i := range hpaList.Items {
							var h = &hpaList.Items[i]
							if h.Name != src.Name {
								continue
							}

							src.DeepCopyInto(h)
							return nil
						}

						return newNotFoundError(cl.Scheme(), src)
					},
				).Times(len(hpaList.Items))
			})

			Describe("fails", func() {
				BeforeEach(func() {
					patchErr = errors.New("error")

					matchErr = HaveOccurred()
				})

				It("should not claim anything and return error", nop)
			})

			Describe("succeeds", func() {
				var expectedHpaList *autoscaling.HorizontalPodAutoscalerList

				BeforeEach(func() {
					patchErr = nil

					claimedHpas = []*autoscaling.HorizontalPodAutoscaler{
						func() *autoscaling.HorizontalPodAutoscaler {
							var h = hpaList.Items[0].DeepCopy()
							Expect(controllerutil.SetControllerReference(controller, h, cl.Scheme())).To(Succeed())
							return h
						}(),
					}

					matchErr = Succeed()

					expectedHpaList = &autoscaling.HorizontalPodAutoscalerList{
						Items: []autoscaling.HorizontalPodAutoscaler{
							*claimedHpas[0], // Adopted
							*func() *autoscaling.HorizontalPodAutoscaler {
								var h = hpaList.Items[1].DeepCopy()
								h.OwnerReferences = nil
								return h
							}(), // Released
						},
					}
				})

				AfterEach(func() {
					Expect(hpaList).To(Equal(expectedHpaList))
				})

				It("shoul claim and release appropriately", nop)
			})
		})
	})

	Describe("ClaimVpas", func() {
		var (
			vpaList     *vpa_api.VerticalPodAutoscalerList
			claimedVpas []*vpa_api.VerticalPodAutoscaler
			matchErr    gomegatypes.GomegaMatcher
		)

		BeforeEach(func() {
			vpaList = &vpa_api.VerticalPodAutoscalerList{
				Items: []vpa_api.VerticalPodAutoscaler{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-yet-owned-but-to-be-adopted",
							Namespace: controller.Namespace,
							Labels: map[string]string{
								"matches": "true",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "already-owned-but-to-be-released",
							Namespace: controller.Namespace,
							Labels: map[string]string{
								"matches": "false",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion:         hvpav1alpha2.SchemeGroupVersionHvpa.String(),
									Kind:               "Hvpa",
									Name:               controller.GetName(),
									UID:                controller.GetUID(),
									BlockOwnerDeletion: pointer.BoolPtr(true),
									Controller:         pointer.BoolPtr(true),
								},
							},
						},
					},
				},
			}

			claimedVpas = nil
		})

		JustBeforeEach(func() {
			var vpas, err = cm.ClaimVpas(vpaList)

			Expect(err).To(matchErr)

			if claimedVpas == nil {
				Expect(vpas).To(BeEmpty())
			} else {
				Expect(vpas).To(Equal(claimedVpas))
			}
		})

		Describe("patch call", func() {
			var patchErr error

			BeforeEach(func() {
				cl.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{}), gomock.Any()).DoAndReturn(
					func(_ context.Context, src *vpa_api.VerticalPodAutoscaler, _ client.Patch) error {
						if patchErr != nil {
							return patchErr
						}

						for i := range vpaList.Items {
							var v = &vpaList.Items[i]
							if v.Name != src.Name {
								continue
							}

							src.DeepCopyInto(v)
							return nil
						}

						return newNotFoundError(cl.Scheme(), src)
					},
				).Times(len(vpaList.Items))
			})

			Describe("fails", func() {
				BeforeEach(func() {
					patchErr = errors.New("error")

					matchErr = HaveOccurred()
				})

				It("should not claim anything and return error", nop)
			})

			Describe("succeeds", func() {
				var expectedVpaList *vpa_api.VerticalPodAutoscalerList

				BeforeEach(func() {
					patchErr = nil

					claimedVpas = []*vpa_api.VerticalPodAutoscaler{
						func() *vpa_api.VerticalPodAutoscaler {
							var v = vpaList.Items[0].DeepCopy()
							Expect(controllerutil.SetControllerReference(controller, v, cl.Scheme())).To(Succeed())
							return v
						}(),
					}

					matchErr = Succeed()

					expectedVpaList = &vpa_api.VerticalPodAutoscalerList{
						Items: []vpa_api.VerticalPodAutoscaler{
							*claimedVpas[0], // Adopted
							*func() *vpa_api.VerticalPodAutoscaler {
								var v = vpaList.Items[1].DeepCopy()
								v.OwnerReferences = nil
								return v
							}(), // Released
						},
					}
				})

				AfterEach(func() {
					Expect(vpaList).To(Equal(expectedVpaList))
				})

				It("shoul claim and release appropriately", nop)
			})
		})
	})
})
