/**
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
	"fmt"
	"time"

	hvpav1alpha2 "github.com/gardener/hvpa-controller/api/v1alpha2"
	mockclient "github.com/gardener/hvpa-controller/mock/controller-runtime/client"
	"github.com/gardener/hvpa-controller/utils"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	vpa_api "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const timeout = time.Second * 5

var (
	unscaled = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("5G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("300m"),
			v1.ResourceMemory: resource.MustParse("200M"),
		},
	}
	scaledSmall = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("270m"),
			v1.ResourceMemory: resource.MustParse("3960000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("2200000k"),
		},
	}
	unscaledSmall = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("3"),
			v1.ResourceMemory: resource.MustParse("4G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("1.8G"),
		},
	}
	scaledLarge = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1225m"),
			v1.ResourceMemory: resource.MustParse("2160000k"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("225m"),
			v1.ResourceMemory: resource.MustParse("1200000k"),
		},
	}
	unscaledLarge = v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("3"),
			v1.ResourceMemory: resource.MustParse("4G"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("150m"),
			v1.ResourceMemory: resource.MustParse("2.2G"),
		},
	}
	target = newTarget("deployment", unscaled, 2)

	minChange = hvpav1alpha2.ScaleParams{
		CPU: hvpav1alpha2.ChangeParams{
			Value:      stringPtr("100m"),
			Percentage: int32Ptr(80),
		},
		Memory: hvpav1alpha2.ChangeParams{
			Value:      stringPtr("100M"),
			Percentage: int32Ptr(80),
		},
	}

	limitScale = hvpav1alpha2.ScaleParams{
		CPU: hvpav1alpha2.ChangeParams{
			Value:      stringPtr("1"),
			Percentage: int32Ptr(80),
		},
		Memory: hvpav1alpha2.ChangeParams{
			Value:      stringPtr("1"),
			Percentage: int32Ptr(80),
		},
	}
)

var _ = Describe("setup", func() {
	var (
		ctx context.Context
		cl  *mockclient.MockClient
		sw  *mockclient.MockStatusWriter
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl := gomock.NewController(GinkgoT())
		cl = mockclient.NewMockClient(mockCtrl)
		sw = mockclient.NewMockStatusWriter(mockCtrl)

		cl.EXPECT().Status().Return(sw).AnyTimes()
		cl.EXPECT().Scheme().Return(scheme.Scheme).AnyTimes()
	})

	Describe("HvpaReconciler", func() {
		var hvpaReconciler *HvpaReconciler

		BeforeEach(func() {
			hvpaReconciler = &HvpaReconciler{
				Client:                cl,
				Scheme:                cl.Scheme(),
				EnableDetailedMetrics: false,
			}

			Expect(hvpaReconciler.AddMetrics(false)).To(Succeed())
		})

		Describe("reconcileHpa", func() {
			var (
				hvpa                    *hvpav1alpha2.Hvpa
				reconcileHpaStatus      *autoscaling.HorizontalPodAutoscalerStatus
				reconcileErr            error
				matchReconcileHpaStatus gomegatypes.GomegaMatcher
				matchReconcileErr       gomegatypes.GomegaMatcher

				expectReconcileToMatch = func(matchHpaStatus, matchErr gomegatypes.GomegaMatcher) {
					matchReconcileHpaStatus = matchHpaStatus
					matchReconcileErr = matchErr
				}

				itShouldReconcile = func() {
					It("should reconcile", func() {
						Expect(reconcileErr).To(matchReconcileErr)
						Expect(reconcileHpaStatus).To(matchReconcileHpaStatus)
					})
				}
			)

			JustBeforeEach(func() {
				reconcileHpaStatus, reconcileErr = hvpaReconciler.reconcileHpa(ctx, hvpa)
			})

			Describe("HVPA instance", func() {
				var targetName = "target"

				BeforeEach(func() {
					hvpa = newHvpa("instance", targetName, "instance", minChange)
				})

				Describe("with invalid HPA selector", func() {
					BeforeEach(func() {
						hvpa.Spec.Hpa.Selector.MatchLabels["*&^%"] = "$#@!"

						expectReconcileToMatch(BeNil(), HaveOccurred())
					})

					itShouldReconcile()
				})

				Describe("when listing HPA", func() {
					var (
						hpas        []*autoscaling.HorizontalPodAutoscaler
						hpaListErr  error
						hpaListCall *gomock.Call
					)

					BeforeEach(func() {
						hpaListCall = cl.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscalerList{}), gomock.Any()).DoAndReturn(
							func(_ context.Context, target *autoscaling.HorizontalPodAutoscalerList, _ ...client.ListOption) error {
								if hpaListErr != nil {
									return hpaListErr
								}

								for _, hpa := range hpas {
									target.Items = append(target.Items, *hpa.DeepCopy())
								}

								return nil
							},
						)
					})

					Describe("fails", func() {
						BeforeEach(func() {
							hpaListErr = errors.New("list HPA error")

							expectReconcileToMatch(BeNil(), MatchError(hpaListErr))
						})

						itShouldReconcile()
					})

					Describe("succeeds", func() {
						BeforeEach(func() {
							hpaListErr = nil
						})

						Describe("without any existing HPA", func() {
							BeforeEach(func() {
								hpas = nil
							})

							Describe("when HPA should not be deployed", func() {
								BeforeEach(func() {
									hvpa.Spec.Hpa.Deploy = false

									expectReconcileToMatch(BeNil(), Succeed())
								})

								itShouldReconcile()
							})

							Describe("when HPA should be deployed", func() {
								BeforeEach(func() {
									hvpa.Spec.Hpa.Deploy = true
								})

								Describe("when creating HPA", func() {
									var hpaCreateErr error

									BeforeEach(func() {
										cl.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{}), gomock.Any()).DoAndReturn(
											func(_ context.Context, src *autoscaling.HorizontalPodAutoscaler, _ ...client.CreateOption) error {
												if hpaCreateErr != nil {
													return hpaCreateErr
												}

												hpas = append(hpas, src.DeepCopy())

												return nil
											},
										).After(hpaListCall)
									})

									Describe("fails", func() {
										BeforeEach(func() {
											hpaCreateErr = errors.New("create HPA error")

											expectReconcileToMatch(Equal(&autoscaling.HorizontalPodAutoscalerStatus{}), MatchError(hpaCreateErr))
										})

										AfterEach(func() {
											Expect(hpas).To(BeEmpty(), "HPA should not be created")
										})

										itShouldReconcile()
									})

									Describe("succeeds", func() {
										var expectedHpa *autoscaling.HorizontalPodAutoscaler

										BeforeEach(func() {
											hpaCreateErr = nil

											expectedHpa = func() *autoscaling.HorizontalPodAutoscaler {
												h, err := getHpaFromHvpa(hvpa)
												Expect(err).ToNot(HaveOccurred())

												controllerutil.SetControllerReference(hvpa, h, cl.Scheme())

												return h
											}()

											expectReconcileToMatch(Equal(&expectedHpa.Status), Succeed())
										})

										AfterEach(func() {
											Expect(hpas).To(ConsistOf(expectedHpa))
										})

										itShouldReconcile()
									})
								})
							})
						})

						Describe("with existing HPAs", func() {
							BeforeEach(func() {
								hpas = nil

								for i := 0; i < 3; i++ {
									h, err := getHpaFromHvpa(hvpa)
									Expect(err).ToNot(HaveOccurred())

									h.Name = fmt.Sprintf("%s%d", h.GenerateName, i)

									hpas = append(hpas, h)
								}
							})

							Describe("when getting HVPA", func() {
								var (
									hvpaGetErr  error
									hvpaGetCall *gomock.Call
								)

								BeforeEach(func() {
									hvpaGetCall = cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(hvpa), gomock.AssignableToTypeOf(hvpa)).DoAndReturn(
										func(_ context.Context, _ types.NamespacedName, target *hvpav1alpha2.Hvpa) error {
											if hvpaGetErr != nil {
												return hvpaGetErr
											}

											hvpa.DeepCopyInto(target)

											return nil
										},
									).After(hpaListCall)
								})

								Describe("fails", func() {
									var expectedHpas []*autoscaling.HorizontalPodAutoscaler

									BeforeEach(func() {
										hvpaGetErr = errors.New("get HVPA error")

										expectedHpas = nil
										for _, h := range hpas {
											expectedHpas = append(expectedHpas, h.DeepCopy())
										}

										expectReconcileToMatch(BeNil(), HaveOccurred())
									})

									AfterEach(func() {
										Expect(hpas).To(Equal(expectedHpas))
									})

									Describe("when HPA should not be deployed", func() {
										BeforeEach(func() {
											hvpa.Spec.Hpa.Deploy = false
										})

										itShouldReconcile()
									})

									Describe("when HPA should be deployed", func() {
										BeforeEach(func() {
											hvpa.Spec.Hpa.Deploy = true
										})

										itShouldReconcile()
									})
								})

								Describe("succeeds", func() {
									BeforeEach(func() {
										hvpaGetErr = nil
									})

									Describe("when patching HPAs", func() {
										var (
											hpaPatchErr  error
											hpaPatchCall *gomock.Call
										)

										BeforeEach(func() {
											hpaPatchCall = cl.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{}), gomock.Any(), gomock.Any()).DoAndReturn(
												func(_ context.Context, src *autoscaling.HorizontalPodAutoscaler, _ client.Patch, _ ...client.PatchOption) error {
													if hpaPatchErr != nil {
														return hpaPatchErr
													}

													Expect(metav1.IsControlledBy(src, hvpa)).To(BeTrue(), "Should have the controller reference set")

													for _, h := range hpas {
														if h.Name != src.Name {
															continue
														}

														src.DeepCopyInto(h)

														return nil
													}

													return newNotFoundError(cl.Scheme(), src)
												},
											).Times(len(hpas)).After(hvpaGetCall)
										})

										Describe("fails", func() {
											var expectedHpas []*autoscaling.HorizontalPodAutoscaler

											BeforeEach(func() {
												hpaPatchErr = errors.New("patch HPA error")

												expectedHpas = nil
												for _, h := range hpas {
													expectedHpas = append(expectedHpas, h.DeepCopy())
												}

												expectReconcileToMatch(BeNil(), HaveOccurred())
											})

											AfterEach(func() {
												Expect(hpas).To(Equal(expectedHpas))
											})

											itShouldReconcile()
										})

										Describe("succeeds", func() {
											BeforeEach(func() {
												hpaPatchErr = nil
											})

											Describe("when getting HPAs for update", func() {
												var hpaGetForUpdateErr error

												BeforeEach(func() {
													cl.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{})).DoAndReturn(
														func(_ context.Context, key types.NamespacedName, target *autoscaling.HorizontalPodAutoscaler) error {
															if hpaGetForUpdateErr != nil {
																return hpaGetForUpdateErr
															}

															for _, h := range hpas {
																if h.Name != key.Name {
																	continue
																}

																h.DeepCopyInto(target)

																return nil
															}

															return newNotFoundError(cl.Scheme(), &autoscaling.HorizontalPodAutoscaler{
																ObjectMeta: metav1.ObjectMeta{
																	Name:      key.Name,
																	Namespace: key.Namespace,
																},
															})
														},
													).Times(len(hpas)).After(hpaPatchCall)
												})

												Describe("succeeds", func() {
													BeforeEach(func() {
														hpaGetForUpdateErr = nil
													})

													Describe("when updating HPAs", func() {
														var (
															hpaUpdateErr  error
															hpaUpdateCall *gomock.Call
														)

														BeforeEach(func() {
															hpaUpdateCall = cl.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{})).DoAndReturn(
																func(_ context.Context, src *autoscaling.HorizontalPodAutoscaler) error {
																	if hpaUpdateErr != nil {
																		return hpaUpdateErr
																	}

																	for _, h := range hpas {
																		if h.Name != src.Name {
																			continue
																		}

																		src.DeepCopyInto(h)
																		return nil
																	}

																	return newNotFoundError(cl.Scheme(), src)
																},
															).Times(len(hpas)).After(hpaPatchCall)
														})

														Describe("succeeds", func() {
															var expectedHpas []*autoscaling.HorizontalPodAutoscaler

															BeforeEach(func() {
																hpaUpdateErr = nil

																expectedHpas = nil
																for _, h := range hpas {
																	var eh = h.DeepCopy()
																	controllerutil.SetControllerReference(hvpa, eh, cl.Scheme())
																	expectedHpas = append(expectedHpas, eh)

																	// Change the spec to make sure the update call is taking effect
																	h.Spec.MaxReplicas = int32(-1)
																	h.Spec.MinReplicas = nil
																	h.Spec.Metrics = nil
																	h.Spec.ScaleTargetRef.APIVersion = "invalid"
																	h.Spec.ScaleTargetRef.Kind = "invalid"
																	h.Spec.ScaleTargetRef.Name = "invalid"
																}
															})

															AfterEach(func() {
																Expect(hpas).To(Equal(expectedHpas))
															})

															Describe("when deleting HPAs", func() {
																var (
																	hpaDeleteErr  error
																	hpaDeleteCall *gomock.Call

																	describeHPADeploymentAndGetCallVariationsWhenDelete = func(spec string, deleteSucceeds bool) {
																		Describe(spec, func() {
																			BeforeEach(func() {
																				if deleteSucceeds {
																					hpaDeleteErr = nil
																				} else {
																					hpaDeleteErr = errors.New("delete HPA error")
																				}
																			})

																			Describe("when HPA should not be deployed", func() {
																				BeforeEach(func() {
																					hvpa.Spec.Hpa.Deploy = false

																					// All HPAs would be deleted
																					hpaDeleteCall.Times(len(hpas))

																					if deleteSucceeds {
																						expectedHpas = nil
																						expectReconcileToMatch(BeNil(), Succeed())
																					} else {
																						expectReconcileToMatch(BeNil(), HaveOccurred())
																					}
																				})

																				itShouldReconcile()
																			})

																			Describe("when HPA should be deployed", func() {
																				BeforeEach(func() {
																					hvpa.Spec.Hpa.Deploy = true

																					// All but one HPAs would be deleted
																					hpaDeleteCall.Times(len(hpas) - 1)

																					if deleteSucceeds {
																						expectedHpas = expectedHpas[:1]
																					}
																				})

																				Describe("when getting HPA", func() {
																					var (
																						hpaGetErr         error
																						expectedHpaStatus *autoscaling.HorizontalPodAutoscalerStatus
																					)

																					BeforeEach(func() {
																						h := hpas[0]
																						cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(h), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{})).DoAndReturn(
																							func(_ context.Context, _ types.NamespacedName, target *autoscaling.HorizontalPodAutoscaler) error {
																								if hpaGetErr != nil {
																									return hpaGetErr
																								}

																								h.DeepCopyInto(target)

																								return nil
																							},
																						)

																						expectedHpaStatus = &autoscaling.HorizontalPodAutoscalerStatus{}
																					})

																					Describe("fails", func() {
																						BeforeEach(func() {
																							hpaGetErr = errors.New("get HPA error")

																							expectReconcileToMatch(Equal(expectedHpaStatus), MatchError(hpaGetErr))
																						})

																						itShouldReconcile()
																					})

																					Describe("succeeds", func() {
																						BeforeEach(func() {
																							hpaGetErr = nil

																							expectReconcileToMatch(Equal(expectedHpaStatus), BeNil())
																						})

																						Describe("with default status", func() {
																							itShouldReconcile()
																						})

																						Describe("with non-default status", func() {
																							BeforeEach(func() {
																								expectedHpaStatus.CurrentReplicas = int32(10)
																								expectedHpaStatus.DesiredReplicas = int32(10)

																								hpas[0].Status = *expectedHpaStatus.DeepCopy()
																								expectedHpas[0].Status = *expectedHpaStatus.DeepCopy()
																							})

																							itShouldReconcile()
																						})
																					})
																				})
																			})
																		})
																	}
																)

																BeforeEach(func() {
																	hpaDeleteCall = cl.EXPECT().Delete(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{}), gomock.Any()).DoAndReturn(
																		func(_ context.Context, src *autoscaling.HorizontalPodAutoscaler, _ ...client.DeleteOption) error {
																			if hpaDeleteErr != nil {
																				return hpaDeleteErr
																			}

																			var hs []*autoscaling.HorizontalPodAutoscaler

																			for _, h := range hpas {
																				if h.Name == src.Name {
																					// Remove the HPA from the hpas slice
																					continue
																				}

																				hs = append(hs, h)
																			}

																			hpas = hs

																			return nil
																		},
																	).After(hpaUpdateCall)
																})

																describeHPADeploymentAndGetCallVariationsWhenDelete("fails", false)
																describeHPADeploymentAndGetCallVariationsWhenDelete("succeeds", true)
															})
														})
													})
												})
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})

		Describe("reconcileVpa", func() {
			var (
				hvpa                    *hvpav1alpha2.Hvpa
				reconcileVpaStatus      *vpa_api.VerticalPodAutoscalerStatus
				reconcileErr            error
				matchReconcileVpaStatus gomegatypes.GomegaMatcher
				matchReconcileErr       gomegatypes.GomegaMatcher

				expectReconcileToMatch = func(matchVpaStatus, matchErr gomegatypes.GomegaMatcher) {
					matchReconcileVpaStatus = matchVpaStatus
					matchReconcileErr = matchErr
				}

				itShouldReconcile = func() {
					It("should reconcile", func() {
						Expect(reconcileErr).To(matchReconcileErr)
						Expect(reconcileVpaStatus).To(matchReconcileVpaStatus)
					})
				}
			)

			JustBeforeEach(func() {
				reconcileVpaStatus, reconcileErr = hvpaReconciler.reconcileVpa(ctx, hvpa)
			})

			Describe("HVPA instance", func() {
				var targetName = "target"

				BeforeEach(func() {
					hvpa = newHvpa("instance", targetName, "instance", minChange)
				})

				Describe("with invalid VPA selector", func() {
					BeforeEach(func() {
						hvpa.Spec.Vpa.Selector.MatchLabels["*&^%"] = "$#@!"

						expectReconcileToMatch(BeNil(), HaveOccurred())
					})

					itShouldReconcile()
				})

				Describe("when listing VPA", func() {
					var (
						vpas        []*vpa_api.VerticalPodAutoscaler
						vpaListErr  error
						vpaListCall *gomock.Call
					)

					BeforeEach(func() {
						vpaListCall = cl.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscalerList{}), gomock.Any()).DoAndReturn(
							func(_ context.Context, target *vpa_api.VerticalPodAutoscalerList, _ ...client.ListOption) error {
								if vpaListErr != nil {
									return vpaListErr
								}

								for _, vpa := range vpas {
									target.Items = append(target.Items, *vpa.DeepCopy())
								}

								return nil
							},
						)
					})

					Describe("fails", func() {
						BeforeEach(func() {
							vpaListErr = errors.New("list VPA error")

							expectReconcileToMatch(BeNil(), MatchError(vpaListErr))
						})

						itShouldReconcile()
					})

					Describe("succeeds", func() {
						BeforeEach(func() {
							vpaListErr = nil
						})

						Describe("without any existing VPA", func() {
							BeforeEach(func() {
								vpas = nil
							})

							Describe("when VPA should not be deployed", func() {
								BeforeEach(func() {
									hvpa.Spec.Vpa.Deploy = false

									expectReconcileToMatch(BeNil(), Succeed())
								})

								itShouldReconcile()
							})

							Describe("when VPA should be deployed", func() {
								BeforeEach(func() {
									hvpa.Spec.Vpa.Deploy = true
								})

								Describe("when creating VPA", func() {
									var vpaCreateErr error

									BeforeEach(func() {
										cl.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{}), gomock.Any()).DoAndReturn(
											func(_ context.Context, src *vpa_api.VerticalPodAutoscaler, _ ...client.CreateOption) error {
												if vpaCreateErr != nil {
													return vpaCreateErr
												}

												vpas = append(vpas, src.DeepCopy())

												return nil
											},
										).After(vpaListCall)
									})

									Describe("fails", func() {
										BeforeEach(func() {
											vpaCreateErr = errors.New("create VPA error")

											expectReconcileToMatch(BeNil(), MatchError(vpaCreateErr))
										})

										AfterEach(func() {
											Expect(vpas).To(BeEmpty(), "VPA should not be created")
										})

										itShouldReconcile()
									})

									Describe("succeeds", func() {
										var expectedVpa *vpa_api.VerticalPodAutoscaler

										BeforeEach(func() {
											vpaCreateErr = nil

											expectedVpa = func() *vpa_api.VerticalPodAutoscaler {
												v, err := getVpaFromHvpa(hvpa)
												Expect(err).ToNot(HaveOccurred())

												controllerutil.SetControllerReference(hvpa, v, cl.Scheme())

												return v
											}()

											expectReconcileToMatch(Equal(&expectedVpa.Status), Succeed())
										})

										AfterEach(func() {
											Expect(vpas).To(ConsistOf(expectedVpa))
										})

										itShouldReconcile()
									})
								})
							})
						})

						Describe("with existing VPAs", func() {
							BeforeEach(func() {
								vpas = nil

								for i := 0; i < 3; i++ {
									v, err := getVpaFromHvpa(hvpa)
									Expect(err).ToNot(HaveOccurred())

									v.Name = fmt.Sprintf("%s%d", v.GenerateName, i)

									vpas = append(vpas, v)
								}
							})

							Describe("when getting HVPA", func() {
								var (
									hvpaGetErr  error
									hvpaGetCall *gomock.Call
								)

								BeforeEach(func() {
									hvpaGetCall = cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(hvpa), gomock.AssignableToTypeOf(hvpa)).DoAndReturn(
										func(_ context.Context, _ types.NamespacedName, target *hvpav1alpha2.Hvpa) error {
											if hvpaGetErr != nil {
												return hvpaGetErr
											}

											hvpa.DeepCopyInto(target)

											return nil
										},
									).After(vpaListCall)
								})

								Describe("fails", func() {
									var expectedVpas []*vpa_api.VerticalPodAutoscaler

									BeforeEach(func() {
										hvpaGetErr = errors.New("get HVPA error")

										expectedVpas = nil
										for _, v := range vpas {
											expectedVpas = append(expectedVpas, v.DeepCopy())
										}

										expectReconcileToMatch(BeNil(), HaveOccurred())
									})

									AfterEach(func() {
										Expect(vpas).To(Equal(expectedVpas))
									})

									Describe("when VPA should not be deployed", func() {
										BeforeEach(func() {
											hvpa.Spec.Hpa.Deploy = false
										})

										itShouldReconcile()
									})

									Describe("when VPA should be deployed", func() {
										BeforeEach(func() {
											hvpa.Spec.Hpa.Deploy = true
										})

										itShouldReconcile()
									})
								})

								Describe("succeeds", func() {
									BeforeEach(func() {
										hvpaGetErr = nil
									})

									Describe("when patching HPAs", func() {
										var (
											vpaPatchErr  error
											vpaPatchCall *gomock.Call
										)

										BeforeEach(func() {
											vpaPatchCall = cl.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{}), gomock.Any(), gomock.Any()).DoAndReturn(
												func(_ context.Context, src *vpa_api.VerticalPodAutoscaler, _ client.Patch, _ ...client.PatchOption) error {
													if vpaPatchErr != nil {
														return vpaPatchErr
													}

													Expect(metav1.IsControlledBy(src, hvpa)).To(BeTrue(), "Should have the controller reference set")

													for _, v := range vpas {
														if v.Name != src.Name {
															continue
														}

														src.DeepCopyInto(v)

														return nil
													}

													return newNotFoundError(cl.Scheme(), src)
												},
											).Times(len(vpas)).After(hvpaGetCall)
										})

										Describe("fails", func() {
											var expectedVpas []*vpa_api.VerticalPodAutoscaler

											BeforeEach(func() {
												vpaPatchErr = errors.New("patch VPA error")

												expectedVpas = nil
												for _, v := range vpas {
													expectedVpas = append(expectedVpas, v.DeepCopy())
												}

												expectReconcileToMatch(BeNil(), HaveOccurred())
											})

											AfterEach(func() {
												Expect(vpas).To(Equal(expectedVpas))
											})

											itShouldReconcile()
										})

										Describe("succeeds", func() {
											BeforeEach(func() {
												vpaPatchErr = nil
											})

											Describe("when getting VPAs for update", func() {
												var vpaGetForUpdateErr error

												BeforeEach(func() {
													cl.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{})).DoAndReturn(
														func(_ context.Context, key types.NamespacedName, target *vpa_api.VerticalPodAutoscaler) error {
															if vpaGetForUpdateErr != nil {
																return vpaGetForUpdateErr
															}

															for _, v := range vpas {
																if v.Name != key.Name {
																	continue
																}

																v.DeepCopyInto(target)

																return nil
															}

															return newNotFoundError(cl.Scheme(), &vpa_api.VerticalPodAutoscaler{
																ObjectMeta: metav1.ObjectMeta{
																	Name:      key.Name,
																	Namespace: key.Namespace,
																},
															})
														},
													).Times(len(vpas)).After(vpaPatchCall)
												})

												Describe("succeeds", func() {
													BeforeEach(func() {
														vpaGetForUpdateErr = nil
													})

													Describe("when updating VPAs", func() {
														var (
															vpaUpdateErr  error
															vpaUpdateCall *gomock.Call
														)

														BeforeEach(func() {
															vpaUpdateCall = cl.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{})).DoAndReturn(
																func(_ context.Context, src *vpa_api.VerticalPodAutoscaler) error {
																	if vpaUpdateErr != nil {
																		return vpaUpdateErr
																	}

																	for _, v := range vpas {
																		if v.Name != src.Name {
																			continue
																		}

																		src.DeepCopyInto(v)
																		return nil
																	}

																	return newNotFoundError(cl.Scheme(), src)
																},
															).Times(len(vpas)).After(vpaPatchCall)
														})

														Describe("succeeds", func() {
															var expectedVpas []*vpa_api.VerticalPodAutoscaler

															BeforeEach(func() {
																vpaUpdateErr = nil

																expectedVpas = nil
																for _, v := range vpas {
																	var ev = v.DeepCopy()
																	controllerutil.SetControllerReference(hvpa, ev, cl.Scheme())
																	expectedVpas = append(expectedVpas, ev)

																	// Change the spec to make sure the update call is taking effect
																	v.Spec.TargetRef.APIVersion = "invalid"
																	v.Spec.TargetRef.Kind = "invalid"
																	v.Spec.TargetRef.Name = "invalid"
																	v.Spec.ResourcePolicy = nil
																	v.Spec.UpdatePolicy = nil
																}
															})

															AfterEach(func() {
																Expect(vpas).To(Equal(expectedVpas))
															})

															Describe("when deleting VPAs", func() {
																var (
																	vpaDeleteErr  error
																	vpaDeleteCall *gomock.Call

																	describeVPADeploymentAndGetCallVariationsWhenDelete = func(spec string, deleteSucceeds bool) {
																		Describe(spec, func() {
																			BeforeEach(func() {
																				if deleteSucceeds {
																					vpaDeleteErr = nil
																				} else {
																					vpaDeleteErr = errors.New("delete VPA error")
																				}
																			})

																			Describe("when VPA should not be deployed", func() {
																				BeforeEach(func() {
																					hvpa.Spec.Vpa.Deploy = false

																					// All HPAs would be deleted
																					vpaDeleteCall.Times(len(vpas))

																					if deleteSucceeds {
																						expectedVpas = nil
																						expectReconcileToMatch(BeNil(), Succeed())
																					} else {
																						expectReconcileToMatch(BeNil(), HaveOccurred())
																					}
																				})

																				itShouldReconcile()
																			})

																			Describe("when VPA should be deployed", func() {
																				BeforeEach(func() {
																					hvpa.Spec.Vpa.Deploy = true

																					// All but one VPAs would be deleted
																					vpaDeleteCall.Times(len(vpas) - 1)

																					if deleteSucceeds {
																						expectedVpas = expectedVpas[:1]
																					}
																				})

																				Describe("when getting VPA", func() {
																					var (
																						vpaGetErr         error
																						expectedVpaStatus *vpa_api.VerticalPodAutoscalerStatus
																					)

																					BeforeEach(func() {
																						v := vpas[0]
																						cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(v), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{})).DoAndReturn(
																							func(_ context.Context, _ types.NamespacedName, target *vpa_api.VerticalPodAutoscaler) error {
																								if vpaGetErr != nil {
																									return vpaGetErr
																								}

																								v.DeepCopyInto(target)

																								return nil
																							},
																						)

																						expectedVpaStatus = &vpa_api.VerticalPodAutoscalerStatus{}
																					})

																					Describe("fails", func() {
																						BeforeEach(func() {
																							vpaGetErr = errors.New("get VPA error")

																							expectReconcileToMatch(Equal(expectedVpaStatus), MatchError(vpaGetErr))
																						})

																						itShouldReconcile()
																					})

																					Describe("succeeds", func() {
																						BeforeEach(func() {
																							vpaGetErr = nil

																							expectReconcileToMatch(Equal(expectedVpaStatus), BeNil())
																						})

																						Describe("with default status", func() {
																							itShouldReconcile()
																						})

																						Describe("with non-default status", func() {
																							BeforeEach(func() {
																								expectedVpaStatus.Recommendation = &vpa_api.RecommendedPodResources{
																									ContainerRecommendations: []vpa_api.RecommendedContainerResources{
																										{ContainerName: "container"},
																									},
																								}

																								vpas[0].Status = *expectedVpaStatus.DeepCopy()
																								expectedVpas[0].Status = *expectedVpaStatus.DeepCopy()
																							})

																							itShouldReconcile()
																						})
																					})
																				})
																			})
																		})
																	}
																)

																BeforeEach(func() {
																	vpaDeleteCall = cl.EXPECT().Delete(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{}), gomock.Any()).DoAndReturn(
																		func(_ context.Context, src *vpa_api.VerticalPodAutoscaler, _ ...client.DeleteOption) error {
																			if vpaDeleteErr != nil {
																				return vpaDeleteErr
																			}

																			var vs []*vpa_api.VerticalPodAutoscaler

																			for _, v := range vpas {
																				if v.Name == src.Name {
																					// Remove the VPA from the hpas slice
																					continue
																				}

																				vs = append(vs, v)
																			}

																			vpas = vs

																			return nil
																		},
																	).After(vpaUpdateCall)
																})

																describeVPADeploymentAndGetCallVariationsWhenDelete("fails", false)
																describeVPADeploymentAndGetCallVariationsWhenDelete("succeeds", true)
															})
														})
													})
												})
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})

		Describe("doScaleTarget", func() {
			var (
				hvpa                                                   *hvpav1alpha2.Hvpa
				targetName                                             = "target"
				target                                                 *unstructured.Unstructured
				targetTyped                                            client.Object
				recommenderFn                                          func(int32, *v1.PodSpec) (int32, *v1.PodSpec, error)
				matchScaleHorizontally, matchScaleVertically, matchErr gomegatypes.GomegaMatcher
				expectedTarget                                         client.Object

				describeTargetUpdateVariations = func(itShouldScale func()) {
					Describe("target update", func() {
						var targetUpdateErr error

						BeforeEach(func() {
							cl.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
								func(_ context.Context, src client.Object) error {
									if targetUpdateErr != nil {
										return targetUpdateErr
									}

									Expect(src.GetName()).To(Equal(targetName))

									Expect(cl.Scheme().Convert(src, target, nil)).To(Succeed())
									return nil
								},
							)
						})

						Describe("fails", func() {
							BeforeEach(func() {
								targetUpdateErr = errors.New("update target error")

								matchErr = MatchError(targetUpdateErr)
							})

							It("should fail and not scale", nop)
						})

						Describe("succeeds", func() {
							BeforeEach(func() {
								targetUpdateErr = nil

								matchErr = Succeed()
							})

							AfterEach(func() {
								var expectedTargetU = &unstructured.Unstructured{}

								Expect(cl.Scheme().Convert(expectedTarget, expectedTargetU, nil)).To(Succeed())

								Expect(target).To(Equal(expectedTargetU))
							})

							itShouldScale()
						})
					})
				}

				describeRecommendationVariations = func(itShouldScale func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec)) {
					Describe("when recommender fails", func() {
						BeforeEach(func() {
							var err = errors.New("recommender error")

							recommenderFn = func(_ int32, _ *v1.PodSpec) (int32, *v1.PodSpec, error) {
								return 0, nil, err
							}

							matchScaleHorizontally = BeFalse()
							matchScaleVertically = BeFalse()
							matchErr = MatchError(err)
						})

						It("should fail and not scale", nop)
					})

					Describe("when recommender does not scale", func() {
						BeforeEach(func() {
							recommenderFn = func(currentReplicas int32, podSpec *v1.PodSpec) (int32, *v1.PodSpec, error) {
								return currentReplicas, podSpec, nil
							}

							matchScaleHorizontally = BeFalse()
							matchScaleVertically = BeFalse()
							matchErr = Succeed()
						})

						It("should not scale", nop)
					})

					Describe("recommender scales", func() {
						var (
							replicasRecommenderFn func(currentReplicas int32) int32
							podSpecRecommenderFn  func(podSpec *v1.PodSpec) *v1.PodSpec
							desiredReplicas       int32
							newPodSpec            *v1.PodSpec

							doItShouldScale = func() {
								itShouldScale(
									func() int32 { return desiredReplicas },
									func() *v1.PodSpec { return newPodSpec.DeepCopy() },
								)
							}

							describeHorizontally = func(specPrefix string) {
								Describe(specPrefix+"horizontally", func() {
									BeforeEach(func() {
										replicasRecommenderFn = func(currentReplicas int32) int32 { return currentReplicas + int32(1) }

										matchScaleHorizontally = BeTrue()
									})

									describeTargetUpdateVariations(doItShouldScale)
								})
							}
						)

						BeforeEach(func() {
							replicasRecommenderFn = func(currentReplicas int32) int32 { return currentReplicas }
							podSpecRecommenderFn = func(podSpec *v1.PodSpec) *v1.PodSpec { return podSpec.DeepCopy() }

							recommenderFn = func(currentReplicas int32, podSpec *v1.PodSpec) (int32, *v1.PodSpec, error) {
								desiredReplicas = replicasRecommenderFn(currentReplicas)
								newPodSpec = podSpecRecommenderFn(podSpec)

								return desiredReplicas, newPodSpec, nil
							}

							matchScaleHorizontally = BeFalse()
							matchScaleVertically = BeFalse()
						})

						describeHorizontally("")

						Describe("vertically", func() {
							BeforeEach(func() {
								podSpecRecommenderFn = func(podSpec *v1.PodSpec) *v1.PodSpec {
									var (
										newPodSpec = podSpec.DeepCopy()
										increment  = resource.MustParse("100")
									)

									for i := range newPodSpec.Containers {
										var c = &newPodSpec.Containers[i]
										for _, rl := range []v1.ResourceList{c.Resources.Requests, c.Resources.Limits} {
											for r := range rl {
												var q = rl[r]
												q.Add(increment)
												rl[r] = q
											}
										}
									}

									return newPodSpec
								}

								matchScaleVertically = BeTrue()
							})

							describeTargetUpdateVariations(doItShouldScale)

							describeHorizontally("and ")
						})
					})
				}
			)

			BeforeEach(func() {
				hvpa = newHvpa("instance", targetName, "instance", minChange)
				target = &unstructured.Unstructured{}
			})

			JustBeforeEach(func() {
				Expect(cl.Scheme().Convert(targetTyped, target, nil)).To(Succeed())

				var scaleHorizontally, scaleVertically, err = hvpaReconciler.doScaleTarget(ctx, hvpa, target, recommenderFn)

				Expect(err).To(matchErr)
				Expect(scaleHorizontally).To(matchScaleHorizontally)
				Expect(scaleVertically).To(matchScaleVertically)
			})

			Describe("podSpec and target ObjectMeta", func() {
				var (
					podSpec       *v1.PodSpec
					targetObjMeta *metav1.ObjectMeta

					itShouldScale = func() {
						It("should scale", nop)
					}
				)

				BeforeEach(func() {
					var (
						base         = resource.MustParse("100")
						nextQuantity = func() resource.Quantity {
							base.Add(resource.MustParse("1"))
							return base.DeepCopy()
						}
					)

					podSpec = &v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container-1",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    nextQuantity(),
										v1.ResourceMemory: nextQuantity(),
									},
									Limits: v1.ResourceList{
										v1.ResourceCPU:    nextQuantity(),
										v1.ResourceMemory: nextQuantity(),
									},
								},
							},
						},
					}

					targetObjMeta = &metav1.ObjectMeta{
						Name:      targetName,
						Namespace: hvpa.Namespace,
					}
				})

				Describe("Deployment", func() {
					var deploy *appsv1.Deployment

					BeforeEach(func() {
						deploy = &appsv1.Deployment{
							ObjectMeta: *targetObjMeta.DeepCopy(),
							Spec: appsv1.DeploymentSpec{
								Replicas: pointer.Int32Ptr(1),
								Template: v1.PodTemplateSpec{
									Spec: *podSpec.DeepCopy(),
								},
							},
						}

						targetTyped = deploy
					})

					describeRecommendationVariations(func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec) {
						BeforeEach(func() {
							var (
								expectedDeploy  = deploy.DeepCopy()
								desiredReplicas = desiredReplicasFn()
								newPodSpec      = newPodSpecFn()
							)

							expectedDeploy.Spec.Replicas = &desiredReplicas
							expectedDeploy.Spec.Template.Spec = *newPodSpec.DeepCopy()

							expectedTarget = expectedDeploy
						})

						itShouldScale()
					})
				})

				Describe("StatefulSet", func() {
					var sts *appsv1.StatefulSet

					BeforeEach(func() {
						sts = &appsv1.StatefulSet{
							ObjectMeta: *targetObjMeta.DeepCopy(),
							Spec: appsv1.StatefulSetSpec{
								Replicas: pointer.Int32Ptr(1),
								Template: v1.PodTemplateSpec{
									Spec: *podSpec.DeepCopy(),
								},
							},
						}

						targetTyped = sts
					})

					describeRecommendationVariations(func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec) {
						BeforeEach(func() {
							var (
								expectedSts     = sts.DeepCopy()
								desiredReplicas = desiredReplicasFn()
								newPodSpec      = newPodSpecFn()
							)

							expectedSts.Spec.Replicas = &desiredReplicas
							expectedSts.Spec.Template.Spec = *newPodSpec.DeepCopy()

							expectedTarget = expectedSts
						})

						itShouldScale()
					})
				})

				Describe("DaemonSet", func() {
					var ds *appsv1.DaemonSet

					BeforeEach(func() {
						ds = &appsv1.DaemonSet{
							ObjectMeta: *targetObjMeta.DeepCopy(),
							Spec: appsv1.DaemonSetSpec{
								Template: v1.PodTemplateSpec{
									Spec: *podSpec.DeepCopy(),
								},
							},
						}

						targetTyped = ds
					})

					describeRecommendationVariations(func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec) {
						BeforeEach(func() {
							var (
								expectedDs = ds.DeepCopy()
								newPodSpec = newPodSpecFn()
							)

							expectedDs.Spec.Template.Spec = *newPodSpec.DeepCopy()

							expectedTarget = expectedDs
						})

						itShouldScale()
					})
				})

				Describe("ReplicaSet", func() {
					var rs *appsv1.ReplicaSet

					BeforeEach(func() {
						rs = &appsv1.ReplicaSet{
							ObjectMeta: *targetObjMeta.DeepCopy(),
							Spec: appsv1.ReplicaSetSpec{
								Replicas: pointer.Int32Ptr(1),
								Template: v1.PodTemplateSpec{
									Spec: *podSpec.DeepCopy(),
								},
							},
						}

						targetTyped = rs
					})

					describeRecommendationVariations(func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec) {
						BeforeEach(func() {
							var (
								expectedRs      = rs.DeepCopy()
								desiredReplicas = desiredReplicasFn()
								newPodSpec      = newPodSpecFn()
							)

							expectedRs.Spec.Replicas = &desiredReplicas
							expectedRs.Spec.Template.Spec = *newPodSpec.DeepCopy()

							expectedTarget = expectedRs
						})

						itShouldScale()
					})
				})

				Describe("ReplicationController", func() {
					var rc *v1.ReplicationController

					BeforeEach(func() {
						rc = &v1.ReplicationController{
							ObjectMeta: *targetObjMeta.DeepCopy(),
							Spec: v1.ReplicationControllerSpec{
								Replicas: pointer.Int32Ptr(1),
								Template: &v1.PodTemplateSpec{
									Spec: *podSpec.DeepCopy(),
								},
							},
						}

						targetTyped = rc
					})

					describeRecommendationVariations(func(desiredReplicasFn func() int32, newPodSpecFn func() *v1.PodSpec) {
						BeforeEach(func() {
							var (
								expectedRc      = rc.DeepCopy()
								desiredReplicas = desiredReplicasFn()
								newPodSpec      = newPodSpecFn()
							)

							expectedRc.Spec.Replicas = &desiredReplicas
							expectedRc.Spec.Template.Spec = *newPodSpec.DeepCopy()

							expectedTarget = expectedRc
						})

						itShouldScale()
					})
				})
			})
		})

		Describe("applyLastApplied", func() {
			var (
				hvpa                   *hvpav1alpha2.Hvpa
				targetName             string
				target, expectedTarget *appsv1.Deployment
				lastApplied            *hvpav1alpha2.ScalingStatus
				matchErr               gomegatypes.GomegaMatcher
			)

			BeforeEach(func() {
				targetName = "target"
				hvpa = newHvpa("instance", targetName, "instance", minChange)
				target = newTarget(targetName, unscaled, 2)
				expectedTarget = target.DeepCopy()
				lastApplied = &hvpav1alpha2.ScalingStatus{}
			})

			JustBeforeEach(func() {
				Expect(hvpaReconciler.applyLastApplied(ctx, lastApplied, hvpa, target)).To(matchErr)
				Expect(target).To(Equal(expectedTarget))
			})

			Describe("when last applied status was default", func() {
				BeforeEach(func() {
					matchErr = Succeed()
				})

				It("should do nothing", nop)
			})

			Describe("when last applied status has empty recommendations", func() {
				BeforeEach(func() {
					lastApplied.HpaStatus.DesiredReplicas = 0

					lastApplied.VpaStatus.ContainerResources = nil
					for i := range target.Spec.Template.Spec.Containers {
						var (
							tc = &target.Spec.Template.Spec.Containers[i]
						)

						lastApplied.VpaStatus.ContainerResources = append(lastApplied.VpaStatus.ContainerResources, hvpav1alpha2.ContainerResources{
							ContainerName: tc.Name,
							Resources:     v1.ResourceRequirements{},
						})
					}

					matchErr = Succeed()
				})

				It("should do nothing", nop)
			})

			Describe("when last applied status has actionable recommendations", func() {
				BeforeEach(func() {
					lastApplied.HpaStatus.DesiredReplicas = *target.Spec.Replicas + 1
					expectedTarget.Spec.Replicas = pointer.Int32(lastApplied.HpaStatus.DesiredReplicas)

					lastApplied.VpaStatus.ContainerResources = nil
					for i := range target.Spec.Template.Spec.Containers {
						var (
							tc = &target.Spec.Template.Spec.Containers[i]
						)

						lastApplied.VpaStatus.ContainerResources = append(lastApplied.VpaStatus.ContainerResources, hvpav1alpha2.ContainerResources{
							ContainerName: tc.Name,
							Resources:     scaledLarge,
						})

						expectedTarget.Spec.Template.Spec.Containers[i].Resources = scaledLarge
					}

					matchErr = Succeed()
				})

				Describe("target update successful", func() {
					BeforeEach(func() {
						cl.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
							func(_ context.Context, src *appsv1.Deployment) error {
								Expect(src).To(Equal(expectedTarget))

								src.DeepCopyInto(target)
								return nil
							},
						)
					})

					It("should succeed", nop)
				})
			})
		})

		Describe("Reconcile", func() {
			var (
				instanceKey    types.NamespacedName
				expectedResult ctrl.Result
				matchErr       gomegatypes.GomegaMatcher
			)

			BeforeEach(func() {
				expectedResult = ctrl.Result{}
			})

			JustBeforeEach(func() {
				var result, err = hvpaReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: instanceKey})

				Expect(err).To(matchErr)
				Expect(result).To(Equal(expectedResult))
			})

			Describe("HVPA instance", func() {
				var (
					targetName     = "target"
					instance       *hvpav1alpha2.Hvpa
					instanceGetErr error
					instanceGet    *gomock.Call
					expectedStatus *hvpav1alpha2.HvpaStatus
				)

				BeforeEach(func() {
					instance = newHvpa("instance", targetName, "instance", minChange)
					instanceKey = client.ObjectKeyFromObject(instance)
					instanceGet = mockGet(cl, instance, func() error { return instanceGetErr })
					expectedStatus = &hvpav1alpha2.HvpaStatus{}
				})

				AfterEach(func() {
					Expect(&instance.Status).To(Equal(expectedStatus))
				})

				Describe("cache variations", func() {
					var (
						matchInstanceInCache gomegatypes.GomegaMatcher

						describeCacheVariations = func(describeIt func(instanceCached bool)) {
							Describe("empty cache", func() {
								describeIt(false)
							})

							Describe("non-empty cache", func() {
								BeforeEach(func() {
									hvpaReconciler.cachedNames = map[string][]*hvpaObj{
										"other": []*hvpaObj{
											{Name: instanceKey.Name},
										},
										instanceKey.Namespace: []*hvpaObj{
											{Name: "other"},
										},
									}
								})

								AfterEach(func() {
									Expect(hvpaReconciler.cachedNames).To(MatchKeys(IgnoreExtras, Keys{
										"other": ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
											"Name": Equal(instanceKey.Name),
										}))),
										instanceKey.Namespace: ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
											"Name": Equal("other"),
										}))),
									}))
								})

								Describe("without instance", func() {
									describeIt(false)
								})

								Describe("with instance", func() {
									BeforeEach(func() {
										hvpaReconciler.cachedNames[instanceKey.Namespace] = append(
											hvpaReconciler.cachedNames[instanceKey.Namespace],
											&hvpaObj{Name: instanceKey.Name},
										)
									})

									describeIt(true)
								})
							})
						}
					)

					BeforeEach(func() {
						matchInstanceInCache = HaveKeyWithValue(instanceKey.Namespace, ContainElement(PointTo(MatchFields(IgnoreExtras, Fields{
							"Name": Equal(instanceKey.Name),
						}))))
					})

					Describe("when instance get fails", func() {
						Describe("with not found error", func() {
							BeforeEach(func() {
								instanceGetErr = newNotFoundError(cl.Scheme(), instance)
							})

							describeCacheVariations(func(_ bool) {
								BeforeEach(func() {
									matchErr = Succeed()
								})

								It("should not cache the instance", func() {
									Expect(hvpaReconciler.cachedNames).ToNot(matchInstanceInCache)
								})
							})
						})

						Describe("with any other error", func() {
							BeforeEach(func() {
								instanceGetErr = errors.New("get instance error")
							})

							describeCacheVariations(func(instanceCached bool) {
								BeforeEach(func() {
									matchErr = MatchError(instanceGetErr)
								})

								if instanceCached {
									It("should keep the instance in the cache", func() {
										Expect(hvpaReconciler.cachedNames).To(matchInstanceInCache)
									})
								} else {
									It("should not cache the instance", func() {
										Expect(hvpaReconciler.cachedNames).ToNot(matchInstanceInCache)
									})
								}
							})
						})
					})

					Describe("when instance get succeeds", func() {
						BeforeEach(func() {
							instanceGetErr = nil
						})

						Describe("target", func() {
							var (
								target       *appsv1.Deployment
								targetGetErr error
								targetGet    *gomock.Call
							)

							BeforeEach(func() {
								target = newTarget(targetName, unscaled, 2)
								targetGet = mockGet(cl, target, func() error { return targetGetErr }).After(instanceGet)
							})

							Describe("when target get fails", func() {
								BeforeEach(func() {
									targetGetErr = errors.New("get target error")
								})

								describeCacheVariations(func(instanceCached bool) {
									BeforeEach(func() {
										matchErr = MatchError(targetGetErr)
									})

									if instanceCached {
										It("should keep the instance in the cache", func() {
											Expect(hvpaReconciler.cachedNames).To(matchInstanceInCache)
										})
									} else {
										It("should not cache the instance", func() {
											Expect(hvpaReconciler.cachedNames).ToNot(matchInstanceInCache)
										})
									}
								})
							})

							Describe("when target get succeeds", func() {
								var (
									expectedTarget *appsv1.Deployment
								)

								BeforeEach(func() {
									targetGetErr = nil
									expectedTarget = target.DeepCopy()
								})

								AfterEach(func() {
									Expect(target).To(Equal(func() *appsv1.Deployment {
										var d = expectedTarget.DeepCopy()
										for i := range d.Spec.Template.Spec.Containers {
											var dc = &d.Spec.Template.Spec.Containers[i]

											dc.Resources = *target.Spec.Template.Spec.Containers[i].Resources.DeepCopy()
										}
										return d
									}()), "Everything except container resources should be unchanged")

									for i := range expectedTarget.Spec.Template.Spec.Containers {
										var ec = &expectedTarget.Spec.Template.Spec.Containers[i]

										Expect(target.Spec.Template.Spec.Containers[i].Resources).To(matcherForResources(&ec.Resources), "Should expected container resources")
									}
								})

								Describe("instance with DeletionTimestamp", func() {
									BeforeEach(func() {
										instance.DeletionTimestamp = &metav1.Time{Time: time.Now()}
									})

									describeCacheVariations(func(_ bool) {
										BeforeEach(func() {
											matchErr = Succeed()
										})

										It("should not cache the instance", func() {
											Expect(hvpaReconciler.cachedNames).ToNot(matchInstanceInCache)
										})
									})
								})

								Describe("instance without DeletionTimestamp", func() {
									var (
										itShouldCacheTheInstance = func() {
											describeCacheVariations(func(_ bool) {
												BeforeEach(func() {
													matchInstanceInCache = HaveKeyWithValue(instanceKey.Namespace, ContainElement(&hvpaObj{
														Name: instanceKey.Name,
														Selector: func() labels.Selector {
															var s, err = metav1.LabelSelectorAsSelector(target.Spec.Selector)
															Expect(err).ToNot(HaveOccurred())

															return s
														}(),
													}))
												})

												It("should cache the instance including the selector", func() {
													Expect(hvpaReconciler.cachedNames).To(matchInstanceInCache)
												})
											})
										}
									)

									BeforeEach(func() {
										expectedResult.RequeueAfter = 1 * time.Minute
									})

									Describe("with no existing HPA and VPA", func() {
										var (
											hpaList, vpaList *gomock.Call
										)

										BeforeEach(func() {
											hpaList = cl.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscalerList{}), gomock.Any()).Return(nil).After(targetGet)
											vpaList = cl.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscalerList{}), gomock.Any()).Return(nil).After(targetGet)
										})

										Describe("when HPA and VPA creation succeeds", func() {
											var (
												hpaStatus            *autoscaling.HorizontalPodAutoscalerStatus
												vpaStatus            *vpa_api.VerticalPodAutoscalerStatus
												hpaCreate, vpaCreate *gomock.Call

												describeHvpaStatusUpdate = func(preReqFn func() []*gomock.Call, describeSuccess func(describeIt func())) {
													Describe("HVPA status update", func() {
														var hvpaStatusUpdateErr error

														BeforeEach(func() {
															var hvpaStatusUpdate = sw.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&hvpav1alpha2.Hvpa{})).DoAndReturn(
																func(_ context.Context, src *hvpav1alpha2.Hvpa) error {
																	if hvpaStatusUpdateErr != nil {
																		return hvpaStatusUpdateErr
																	}

																	src.Status.DeepCopyInto(&instance.Status)
																	return nil
																},
															)

															for _, preReq := range preReqFn() {
																hvpaStatusUpdate.After(preReq)
															}
														})

														Describe("fails", func() {
															BeforeEach(func() {
																hvpaStatusUpdateErr = errors.New("update HVPA status error")

																matchErr = MatchError(hvpaStatusUpdateErr)
															})

															itShouldCacheTheInstance()
														})

														Describe("succeeds", func() {
															BeforeEach(func() {
																hvpaStatusUpdateErr = nil

																expectedStatus.TargetSelector = func() *string {
																	var selector, err = metav1.LabelSelectorAsSelector(target.Spec.Selector)
																	Expect(err).ToNot(HaveOccurred())
																	return pointer.StringPtr(selector.String())
																}()

																matchErr = Succeed()
															})

															if describeSuccess != nil {
																describeSuccess(itShouldCacheTheInstance)
															} else {
																itShouldCacheTheInstance()
															}
														})
													})
												}

												describeTargetUpdate = func(describeSuccess, describeFailure func(preReqFn func() []*gomock.Call)) {
													Describe("target update", func() {
														var (
															targetUpdateErr error
															targetUpdate    *gomock.Call
														)

														BeforeEach(func() {
															targetUpdate = cl.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&appsv1.Deployment{})).DoAndReturn(
																func(_ context.Context, src *appsv1.Deployment) error {
																	if targetUpdateErr != nil {
																		return targetUpdateErr
																	}

																	src.DeepCopyInto(target)
																	return nil
																},
															).After(hpaCreate).After(vpaCreate)
														})

														Describe("fails", func() {
															BeforeEach(func() {
																targetUpdateErr = errors.New("target update error")
																expectedResult.RequeueAfter = time.Duration(0)
																matchErr = MatchError(targetUpdateErr)
															})

															if describeFailure != nil {
																describeFailure(func() []*gomock.Call {
																	return []*gomock.Call{targetUpdate}
																})
															} else {
																itShouldCacheTheInstance()
															}
														})

														Describe("succeeds", func() {
															BeforeEach(func() {
																targetUpdateErr = nil
															})

															describeSuccess(func() []*gomock.Call {
																return []*gomock.Call{targetUpdate}
															})
														})
													})
												}
											)

											BeforeEach(func() {
												hpaStatus = &autoscaling.HorizontalPodAutoscalerStatus{}
												hpaCreate = cl.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&autoscaling.HorizontalPodAutoscaler{}), gomock.Any()).DoAndReturn(
													func(_ context.Context, h *autoscaling.HorizontalPodAutoscaler, _ ...client.CreateOption) error {
														h.Status = *hpaStatus.DeepCopy()
														return nil
													},
												).After(hpaList)

												vpaStatus = &vpa_api.VerticalPodAutoscalerStatus{}
												vpaCreate = cl.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&vpa_api.VerticalPodAutoscaler{}), gomock.Any()).DoAndReturn(
													func(_ context.Context, v *vpa_api.VerticalPodAutoscaler, _ ...client.CreateOption) error {
														vpaStatus.DeepCopyInto(&v.Status)
														return nil
													},
												).After(vpaList)
											})

											Describe("with default status", func() {
												Describe("with no lastApplied recommendations", func() {
													describeHvpaStatusUpdate(func() []*gomock.Call {
														return []*gomock.Call{hpaCreate, vpaCreate}
													}, nil)
												})

												Describe("with some lastApplied recommendations", func() {
													BeforeEach(func() {
														instance.Status.LastScaling.HpaStatus.DesiredReplicas = *target.Spec.Replicas + 1
														instance.Status.LastScaling.DeepCopyInto(&expectedStatus.LastScaling)
													})

													describeTargetUpdate(
														// Success
														func(preReqFn func() []*gomock.Call) {
															BeforeEach(func() {
																expectedTarget.Spec.Replicas = pointer.Int32(instance.Status.LastScaling.HpaStatus.DesiredReplicas)
															})

															describeHvpaStatusUpdate(preReqFn, nil)
														},
														// Failure
														func(_ func() []*gomock.Call) {
															BeforeEach(func() {
																// The requeue period is longer if reapplying existing recommendation fails with no new recommendations available.
																expectedResult.RequeueAfter = 1 * time.Minute
															})

															itShouldCacheTheInstance()
														},
													)
												})
											})

											Describe("with status containing recommendations", func() {
												BeforeEach(func() {
													hpaStatus.DesiredReplicas = *target.Spec.Replicas + 1
													vpaStatus.Conditions = []vpa_api.VerticalPodAutoscalerCondition{
														{
															Type:   vpa_api.RecommendationProvided,
															Status: v1.ConditionTrue,
														},
													}
													vpaStatus.Recommendation = &vpa_api.RecommendedPodResources{
														ContainerRecommendations: []vpa_api.RecommendedContainerResources{
															{
																ContainerName: target.Spec.Template.Spec.Containers[0].Name,
																Target:        scaledLarge.Requests.DeepCopy(),
															},
														},
													}
												})

												describeTargetUpdate(
													func(preReqFn func() []*gomock.Call) {
														BeforeEach(func() {
															var (
																tRes = target.Spec.Template.Spec.Containers[0].Resources.DeepCopy()
																ec   = &expectedTarget.Spec.Template.Spec.Containers[0]
															)

															ec.Resources = *tRes.DeepCopy()
															ec.Resources.Requests = scaledLarge.Requests.DeepCopy()
															ec.Resources.Requests[v1.ResourceCPU] = resource.MustParse("450m")
														})

														describeHvpaStatusUpdate(preReqFn, func(describeIt func()) {
															BeforeEach(func() {
																var ec = &expectedTarget.Spec.Template.Spec.Containers[0]

																expectedStatus.LastProcessedRecommendations = hvpav1alpha2.ScalingStatus{
																	HpaStatus: hvpav1alpha2.HpaStatus{
																		CurrentReplicas: hpaStatus.CurrentReplicas,
																		DesiredReplicas: hpaStatus.DesiredReplicas,
																	},
																	VpaStatus: hvpav1alpha2.VpaStatus{
																		ContainerResources: []hvpav1alpha2.ContainerResources{
																			{
																				ContainerName: ec.Name,
																				Resources:     *ec.Resources.DeepCopy(),
																			},
																		},
																	},
																}

																expectedStatus.LastScaling = hvpav1alpha2.ScalingStatus{
																	VpaStatus: hvpav1alpha2.VpaStatus{
																		ContainerResources: []hvpav1alpha2.ContainerResources{
																			{
																				ContainerName: ec.Name,
																				Resources: v1.ResourceRequirements{
																					Requests: ec.Resources.Requests.DeepCopy(),
																				},
																			},
																		},
																	},
																}

																describeIt()
															})
														})
													},
													// Use default failure description
													nil,
												)
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})

		Describe("getPodEventHandler", func() {
			var (
				ns           = "default"
				targetName   = "target"
				eventHandler handler.EventHandler
				pod          *v1.Pod

				itShouldDoNothing = func() {
					It("should do nothing", nop)
				}
			)

			BeforeEach(func() {
				eventHandler = hvpaReconciler.getPodEventHandler()

				pod = &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      targetName + "-pod",
						Namespace: ns,
					},
				}
			})

			JustBeforeEach(func() {
				eventHandler.Generic(event.GenericEvent{Object: pod}, nil)
			})

			Describe("when pod is not yet scheduled", func() {
				itShouldDoNothing()
			})

			Describe("when pod is scheduled", func() {
				BeforeEach(func() {
					pod.Spec.NodeName = "node"
				})

				Describe("when cache is empty", func() {
					itShouldDoNothing()
				})

				Describe("when cache is not empty", func() {
					var (
						hvpaName    = "instance"
						cachedEntry *hvpaObj
					)

					BeforeEach(func() {
						cachedEntry = &hvpaObj{Name: hvpaName}

						hvpaReconciler.cachedNames = map[string][]*hvpaObj{
							ns: []*hvpaObj{cachedEntry},
						}
					})

					Describe("with no entries with selector matching the pod labels", func() {
						BeforeEach(func() {
							cachedEntry.Selector = labels.Nothing()
						})

						itShouldDoNothing()
					})

					Describe("with an entry with selector matching the pod labels", func() {
						BeforeEach(func() {
							cachedEntry.Selector = labels.Everything()
						})

						Describe("getting HVPA", func() {
							var (
								hvpa        *hvpav1alpha2.Hvpa
								hvpaGetErr  error
								hvpaGetCall *gomock.Call
							)

							BeforeEach(func() {
								hvpa = &hvpav1alpha2.Hvpa{
									ObjectMeta: metav1.ObjectMeta{
										Name:      hvpaName,
										Namespace: ns,
									},
								}

								hvpaGetCall = cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(hvpa), gomock.AssignableToTypeOf(hvpa)).DoAndReturn(
									func(_ context.Context, _ types.NamespacedName, target *hvpav1alpha2.Hvpa) error {
										if hvpaGetErr != nil {
											return hvpaGetErr
										}

										hvpa.DeepCopyInto(target)
										return nil
									},
								)
							})

							Describe("fails", func() {
								BeforeEach(func() {
									hvpaGetErr = errors.New("get HVPA error")
								})

								itShouldDoNothing()
							})

							Describe("succeeds", func() {
								BeforeEach(func() {
									hvpaGetErr = nil
								})

								Describe("with no targetRef", func() {
									BeforeEach(func() {
										hvpa.Spec.TargetRef = nil
									})

									itShouldDoNothing()
								})

								Describe("with invalid targetRef", func() {
									BeforeEach(func() {
										hvpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{}
									})

									itShouldDoNothing()
								})

								Describe("with unsupported targetRef", func() {
									BeforeEach(func() {
										hvpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{
											APIVersion: v1.SchemeGroupVersion.String(),
											Kind:       "Pod",
											Name:       pod.Name,
										}

										Describe("when getting target succeeds", func() {
											BeforeEach(func() {
												cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(pod), gomock.AssignableToTypeOf(pod)).DoAndReturn(
													func(_ context.Context, _ types.NamespacedName, target *v1.Pod) error {
														pod.DeepCopyInto(target)
														return nil
													},
												)
											})

											itShouldDoNothing()
										})
									})
								})

								Describe("with supported targetRef", func() {
									BeforeEach(func() {
										hvpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{
											APIVersion: appsv1.SchemeGroupVersion.String(),
											Kind:       "Deployment",
											Name:       targetName,
										}
									})

									Describe("getting target", func() {
										var (
											target        *appsv1.Deployment
											targetGetErr  error
											targetGetCall *gomock.Call
										)

										BeforeEach(func() {
											target = &appsv1.Deployment{
												ObjectMeta: metav1.ObjectMeta{
													Name:      targetName,
													Namespace: ns,
												},
											}

											targetGetCall = cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(target), gomock.AssignableToTypeOf(target)).DoAndReturn(
												func(_ context.Context, _ types.NamespacedName, t *appsv1.Deployment) error {
													if targetGetErr != nil {
														return targetGetErr
													}

													target.DeepCopyInto(t)
													return nil
												},
											).After(hvpaGetCall)
										})

										Describe("fails", func() {
											BeforeEach(func() {
												targetGetErr = errors.New("get target error")
											})

											itShouldDoNothing()
										})

										Describe("succeeds", func() {
											BeforeEach(func() {
												targetGetErr = nil
											})

											Describe("with no replicas", func() {
												BeforeEach(func() {
													target.Spec.Replicas = pointer.Int32Ptr(0)
												})

												itShouldDoNothing()
											})

											Describe("when pod resources do not match the resources in the target pod template", func() {
												BeforeEach(func() {
													target.Spec.Template.Spec.Containers = []v1.Container{
														{Name: "target", Resources: scaledLarge},
													}

													pod.Spec.Containers = []v1.Container{
														{Name: "target", Resources: unscaled},
													}
												})

												itShouldDoNothing()
											})

											Describe("when pod resources match the resources in the target pod template", func() {
												Describe("when overrideScaleUpStabilization is already set", func() {
													BeforeEach(func() {
														hvpa.Status.OverrideScaleUpStabilization = true
													})

													itShouldDoNothing()
												})

												Describe("when overrideScaleUpStabilization is not yet set", func() {
													var (
														describeHVPAStatusUpdate = func(preReqFn func() *gomock.Call) {
															Describe("updating HVPA status", func() {
																var hvpaStatusUpdateErr error

																BeforeEach(func() {
																	sw.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(hvpa)).DoAndReturn(
																		func(_ context.Context, src *hvpav1alpha2.Hvpa) error {
																			if hvpaStatusUpdateErr != nil {
																				return hvpaStatusUpdateErr
																			}

																			src.DeepCopyInto(hvpa)
																			return nil
																		},
																	).After(preReqFn())
																})

																Describe("fails", func() {
																	BeforeEach(func() {
																		hvpaStatusUpdateErr = errors.New("update HVPA status error")
																	})

																	itShouldDoNothing()
																})

																Describe("succeeds", func() {
																	BeforeEach(func() {
																		hvpaStatusUpdateErr = nil
																	})

																	It("should set overrideScaleUpStabilization in the HVPA status", func() {
																		Expect(hvpa.Status.OverrideScaleUpStabilization).To(BeTrue())
																	})
																})
															})
														}
													)

													BeforeEach(func() {
														hvpa.Status.OverrideScaleUpStabilization = false
													})

													Describe("when pod has been evicted", func() {
														BeforeEach(func() {
															pod.Status.Reason = "Evicted"
														})

														Describe("getting node", func() {
															var (
																node        *v1.Node
																nodeGetErr  error
																nodeGetCall *gomock.Call
															)

															BeforeEach(func() {
																node = &v1.Node{
																	ObjectMeta: metav1.ObjectMeta{
																		Name: pod.Spec.NodeName,
																	},
																}

																nodeGetCall = cl.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(node), gomock.AssignableToTypeOf(node)).DoAndReturn(
																	func(_ context.Context, _ types.NamespacedName, t *v1.Node) error {
																		if nodeGetErr != nil {
																			return nodeGetErr
																		}

																		node.DeepCopyInto(t)
																		return nil
																	},
																).After(targetGetCall)
															})

															Describe("fails", func() {
																BeforeEach(func() {
																	nodeGetErr = errors.New("get node error")
																})

																itShouldDoNothing()
															})

															Describe("succeeds", func() {
																BeforeEach(func() {
																	nodeGetErr = nil
																})

																Describe("node has sufficient memory", func() {
																	itShouldDoNothing()
																})

																Describe("node has insufficient memory", func() {
																	BeforeEach(func() {
																		node.Status.Conditions = []v1.NodeCondition{
																			{
																				Reason: "KubeletHasInsufficientMemory",
																				Status: v1.ConditionTrue,
																			},
																		}
																	})

																	describeHVPAStatusUpdate(
																		func() *gomock.Call { return nodeGetCall },
																	)
																})
															})
														})
													})

													Describe("when pod was not evicted", func() {
														Describe("does not have containerStatuses", func() {
															itShouldDoNothing()
														})

														Describe("has containerStatuses", func() {
															BeforeEach(func() {
																pod.Status.ContainerStatuses = []v1.ContainerStatus{
																	{Name: targetName},
																}
															})

															Describe("not OOMKilled after last scaling by HVPA", func() {
																itShouldDoNothing()
															})

															Describe("OOMKilled after last scaling by HVPA", func() {
																BeforeEach(func() {
																	var (
																		cs = &pod.Status.ContainerStatuses[0]
																		ls = &hvpa.Status.LastScaling
																	)

																	cs.RestartCount = int32(1)
																	cs.LastTerminationState.Terminated = &v1.ContainerStateTerminated{
																		Reason:     "OOMKilled",
																		FinishedAt: metav1.Now(),
																	}

																	ls.LastUpdated = &metav1.Time{
																		Time: cs.LastTerminationState.Terminated.FinishedAt.Time.Add(
																			-1 * time.Minute,
																		),
																	}
																})

																Describe("with container scaling mode off", func() {
																	BeforeEach(func() {
																		var modeOff = vpa_api.ContainerScalingModeOff

																		hvpa.Spec.Vpa.Template.Spec.ResourcePolicy = &vpa_api.PodResourcePolicy{
																			ContainerPolicies: []vpa_api.ContainerResourcePolicy{
																				{ContainerName: targetName, Mode: &modeOff},
																			},
																		}
																	})

																	itShouldDoNothing()
																})

																Describe("without container scaling mode off", func() {
																	describeHVPAStatusUpdate(
																		func() *gomock.Call { return targetGetCall },
																	)
																})
															})
														})
													})
												})
											})
										})
									})
								})
							})
						})
					})
				})
			})
		})
	})
})

var _ = Describe("nil ResourceList", func() {
	var (
		rl                   v1.ResourceList
		matchDefaultQuantity = EqualQuantity(resource.Quantity{})
	)

	BeforeEach(func() {
		rl = nil
	})

	It("should return default CPU", func() {
		Expect(rl.Cpu()).To(PointTo(matchDefaultQuantity))
	})

	It("should return default memory", func() {
		Expect(rl.Memory()).To(PointTo(matchDefaultQuantity))
	})
})

var _ = Describe("empty resource.Quantity", func() {
	var q = &resource.Quantity{}

	It("should be zero", func() {
		Expect(q.IsZero()).To(BeTrue())
	})
})

var mgr manager.Manager

var _ = XDescribe("#TestReconcile", func() {

	DescribeTable("##ReconcileHPAandVPA",
		func(instance *hvpav1alpha2.Hvpa) {
			deploytest := newTarget("deploy-test-1", unscaled, 2)

			c := mgr.GetClient()
			// Create the test deployment
			err := c.Create(context.TODO(), deploytest)
			Expect(err).NotTo(HaveOccurred())

			// Create the Hvpa object and expect the Reconcile and HPA to be created
			err = c.Create(context.TODO(), instance)
			Expect(err).NotTo(HaveOccurred())
			defer c.Delete(context.TODO(), instance)

			hpa := testHpaReconcile(c)
			vpa := testVpaReconcile(c)
			testOverrideStabilization(c, deploytest, instance)

			testScalingOnVPAReco(c, vpa, instance, deploytest)

			// Status cleanup to prevent blocking due to stabilization window
			hvpa := &hvpav1alpha2.Hvpa{}
			Eventually(func() error {
				if err = c.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
					return err
				}
				hvpa.Status.LastScaling.LastUpdated = nil
				if err = c.Status().Update(context.TODO(), hvpa); err != nil {
					return err
				}
				return nil
			}, timeout).Should(Succeed())

			testScalingOnHPAReco(c, hpa, instance, deploytest)

			testNoScalingOnHvpaSpecUpdate(c, instance)

			// Manually delete HPA & VPA since GC isn't enabled in the test control plane
			Eventually(func() error { return c.Delete(context.TODO(), hpa) }, timeout).
				Should(MatchError(fmt.Sprintf("horizontalpodautoscalers.autoscaling \"%s\" not found", hpa.Name)))
			Eventually(func() error { return c.Delete(context.TODO(), vpa) }, timeout).
				Should(MatchError(fmt.Sprintf("verticalpodautoscalers.autoscaling.k8s.io \"%s\" not found", vpa.Name)))

			// Delete the test deployment
			Expect(c.Delete(context.TODO(), deploytest)).NotTo(HaveOccurred())
		},
		Entry("hvpa", newHvpa("hvpa-1", "deploy-test-1", "label-1", minChange)),
	)

	Describe("#ScaleTests", func() {
		type setup struct {
			hvpa      *hvpav1alpha2.Hvpa
			hpaStatus *autoscaling.HorizontalPodAutoscalerStatus
			vpaStatus *vpa_api.VerticalPodAutoscalerStatus
			target    *appsv1.Deployment
		}
		type expect struct {
			desiredReplicas int32
			resourceChange  bool
			resources       v1.ResourceRequirements
			blockedReasons  []hvpav1alpha2.BlockingReason
		}
		type action struct {
			maintenanceWindow       *hvpav1alpha2.MaintenanceTimeWindow
			updateMode              string
			limitScaling            hvpav1alpha2.ScaleParams
			scaleIntervals          []hvpav1alpha2.ScaleInterval
			vpaStatusCondition      []vpa_api.VerticalPodAutoscalerCondition
			baseResourcesPerReplica hvpav1alpha2.ResourceChangeParams
		}
		type data struct {
			setup  setup
			action action
			expect expect
		}

		DescribeTable("##ScaleTestScenarios",
			func(data *data) {
				hvpa := data.setup.hvpa
				hpaStatus := data.setup.hpaStatus
				vpaStatus := data.setup.vpaStatus
				target := data.setup.target

				hvpa.Spec.Vpa.LimitsRequestsGapScaleParams = data.action.limitScaling
				if data.action.maintenanceWindow != nil {
					hvpa.Spec.MaintenanceTimeWindow = data.action.maintenanceWindow
				}
				if data.action.updateMode != "" {
					hvpa.Spec.ScaleUp.UpdatePolicy.UpdateMode = &data.action.updateMode
					hvpa.Spec.ScaleDown.UpdatePolicy.UpdateMode = &data.action.updateMode
				}
				if data.action.vpaStatusCondition != nil {
					vpaStatus.Conditions = append(data.action.vpaStatusCondition, vpaStatus.Conditions...)
				}
				if data.action.scaleIntervals != nil {
					hvpa.Spec.ScaleIntervals = data.action.scaleIntervals
				}
				if data.action.baseResourcesPerReplica != nil {
					hvpa.Spec.BaseResourcesPerReplica = data.action.baseResourcesPerReplica
				}
				if data.action.vpaStatusCondition != nil {
					vpaStatus.Conditions = append(data.action.vpaStatusCondition, vpaStatus.Conditions...)
				}

				scaledStatus, newPodSpec, resourceChanged, blockedScaling, err := getScalingRecommendations(hpaStatus, vpaStatus, hvpa, &target.Spec.Template.Spec, *target.Spec.Replicas)

				if data.action.vpaStatusCondition != nil {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(resourceChanged).To(Equal(data.expect.resourceChange))

				Expect(len(*blockedScaling)).To(Equal(len(data.expect.blockedReasons)))
				if len(data.expect.blockedReasons) != 0 {
					for i, blockedScaling := range *blockedScaling {
						Expect(blockedScaling.Reason).To(Equal(data.expect.blockedReasons[i]))
					}
				}

				if data.expect.desiredReplicas == *target.Spec.Replicas && data.expect.resourceChange == false {
					Expect(scaledStatus).To(BeNil())
				} else {
					Expect(scaledStatus.HpaStatus.DesiredReplicas).To(Equal(data.expect.desiredReplicas))
				}
				if data.expect.resourceChange {
					Expect(newPodSpec).NotTo(BeNil())
					Expect(newPodSpec.Containers[0].Resources).To(Equal(data.expect.resources))
				} else {
					Expect(newPodSpec).To(BeNil())
				}
			},

			Entry("UpdateMode Auto, scale up, paradoxical scaling, replicas increases, resources per replica decrease is blocked", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "2.2G", "150m"),
					target:    newTarget("deployment", unscaledSmall, 1),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  false,
					resources:       unscaledSmall,
					blockedReasons:  []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scaled down, no scaling because of paradoxical scaling recommendations", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha2.BlockingReason{
						hvpav1alpha2.BlockingReasonParadoxicalScaling,
					},
				},
			}),
			Entry("UpdateMode Auto, overall scale up, paradoxical scaling, but replicas decrease - should not be considered paradoxical", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 3),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("750m"),
							"memory": resource.MustParse("6000000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, overall scale down, paradoxical scaling, but replicas increase - should not be considered paradoxical", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "1500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("15"),
								v1.ResourceMemory: resource.MustParse("20G"),
							},
						}, 1),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("750m"),
							"memory": resource.MustParse("2000000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale up blocked due to minChange", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.89G", "150m"),
					target:    newTarget("deployment", unscaledSmall, 1),
				},
				action: action{
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 1,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha2.BlockingReason{
						hvpav1alpha2.BlockingReasonMinChange,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, blocked scaling", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "0.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha2.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()+3)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+4)%24, 0, 0).Formatted(),
					},
					updateMode: hvpav1alpha2.UpdateModeMaintenanceWindow,
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  false,
					blockedReasons: []hvpav1alpha2.BlockingReason{
						hvpav1alpha2.BlockingReasonMaintenanceWindow,
					},
				},
			}),
			Entry("UpdateMode maintenanceWindow, scale down", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "0.8G", "150m"),
					target:    newTarget("deployment", unscaledLarge, 3),
				},
				action: action{
					maintenanceWindow: &hvpav1alpha2.MaintenanceTimeWindow{
						Begin: utils.NewMaintenanceTime((time.Now().UTC().Hour()-1)%24, 0, 0).Formatted(),
						End:   utils.NewMaintenanceTime((time.Now().UTC().Hour()+1)%24, 0, 0).Formatted(),
					},
					updateMode:   hvpav1alpha2.UpdateModeMaintenanceWindow,
					limitScaling: limitScale,
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources:       scaledLarge,
					blockedReasons:  []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("VPA unsupported condition", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "3G", "500m"),
					target:    target,
				},
				action: action{
					vpaStatusCondition: []vpa_api.VerticalPodAutoscalerCondition{
						{
							Type:   vpa_api.ConfigUnsupported,
							Status: v1.ConditionTrue,
						},
					},
				},
				expect: expect{
					resourceChange: false,
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale down hysteresis based on scaling intervals overlap", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "5.486G", "2.828"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"cpu":    resource.MustParse("8"),
								"memory": resource.MustParse("10G"),
							},
						}, 3),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					blockedReasons:  []hvpav1alpha2.BlockingReason{},
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("2828m"),
							"memory": resource.MustParse("5486000k"),
						},
					},
				},
			}),
			Entry("UpdateMode Auto, scale up, no bucket switch even when scaling intervals overlap", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "13G", "9.5"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"cpu":    resource.MustParse("8"),
								"memory": resource.MustParse("10G"),
							},
						}, 3),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					blockedReasons:  []hvpav1alpha2.BlockingReason{},
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("9500m"),
							"memory": resource.MustParse("13000000k"),
						},
					},
				},
			}),
			Entry("UpdateMode Auto, scale up, base resource usage adjusted", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 1),
				},
				action: action{
					baseResourcesPerReplica: hvpav1alpha2.ResourceChangeParams{
						"cpu": hvpav1alpha2.ChangeParams{
							Value: stringPtr("100m"),
						},
						"memory": hvpav1alpha2.ChangeParams{
							Percentage: int32Ptr(10),
						},
					},
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("300m"),
							"memory": resource.MustParse("2110000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, prevent scale down below minAllowed, allow scale up after last scaleInterval's maxCPU/maxMem", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "75G", "0.134"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("0.613"),
								v1.ResourceMemory: resource.MustParse("11838449000"),
							},
						}, 4),
				},
				expect: expect{
					desiredReplicas: 6,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("100m"),
							"memory": resource.MustParse("50000000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale down, base resource usage adjusted", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "0.8G", "150m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("2.2G"),
							},
						}, 3),
				},
				action: action{
					baseResourcesPerReplica: hvpav1alpha2.ResourceChangeParams{
						"cpu": hvpav1alpha2.ChangeParams{
							Value: stringPtr("100m"),
						},
						"memory": hvpav1alpha2.ChangeParams{
							Percentage: int32Ptr(10),
						},
					},
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("175m"),
							"memory": resource.MustParse("990000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("UpdateMode Auto, scale up, nil base resource usage", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("1.8G"),
							},
						}, 1),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("250m"),
							"memory": resource.MustParse("2000000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale up 1", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.2G", "500m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("150m"),
								v1.ResourceMemory: resource.MustParse("0.4G"),
							},
						}, 1),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("250m"),
							"memory": resource.MustParse("600000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale up 2", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "2.4G", "0.9"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("250m"),
								v1.ResourceMemory: resource.MustParse("600000k"),
							},
						}, 2),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("600m"),
							"memory": resource.MustParse("1600000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale up 3", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "3G", "1.2"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("600m"),
								v1.ResourceMemory: resource.MustParse("1600000k"),
							},
						}, 3),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 4,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("900m"),
							"memory": resource.MustParse("2250000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale up 4", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "4.2G", "1.6"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("900m"),
								v1.ResourceMemory: resource.MustParse("2250000k"),
							},
						}, 4),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 5,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("1280m"),
							"memory": resource.MustParse("3360000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale up 5", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "6.6G", "3"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1280m"),
								v1.ResourceMemory: resource.MustParse("3360000k"),
							},
						}, 5),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 6,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("2500m"),
							"memory": resource.MustParse("5500000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale down 5", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "3G", "900m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2500m"),
								v1.ResourceMemory: resource.MustParse("5500000k"),
							},
						}, 6),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 5,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("1080m"),
							"memory": resource.MustParse("3600000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale down 4", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "2G", "600m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1080m"),
								v1.ResourceMemory: resource.MustParse("3600000k"),
							},
						}, 5),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 4,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("750m"),
							"memory": resource.MustParse("2500000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale down 3", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "1.2G", "420m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("750m"),
								v1.ResourceMemory: resource.MustParse("2500000k"),
							},
						}, 4),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 3,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("560m"),
							"memory": resource.MustParse("1600000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale down 2", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "900M", "300m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("560m"),
								v1.ResourceMemory: resource.MustParse("1600000k"),
							},
						}, 3),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 2,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("450m"),
							"memory": resource.MustParse("1350000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
			Entry("Full round scale test - scale down 1", &data{
				setup: setup{
					hvpa:      newHvpa("hvpa-2", target.GetName(), "label-2", minChange),
					hpaStatus: nil,
					vpaStatus: newVpaStatus("deployment", "400M", "150m"),
					target: newTarget("deployment",
						v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("450m"),
								v1.ResourceMemory: resource.MustParse("1350000k"),
							},
						}, 2),
				},
				action: action{
					scaleIntervals: newScaleInterval(),
				},
				expect: expect{
					desiredReplicas: 1,
					resourceChange:  true,
					resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse("300m"),
							"memory": resource.MustParse("800000k"),
						},
					},
					blockedReasons: []hvpav1alpha2.BlockingReason{},
				},
			}),
		)
	})
})

func testHpaReconcile(k8sClient client.Client) *autoscaling.HorizontalPodAutoscaler {
	hpaList := &autoscaling.HorizontalPodAutoscalerList{}
	hpa := &autoscaling.HorizontalPodAutoscaler{}
	Eventually(func() error {
		num := 0
		k8sClient.List(context.TODO(), hpaList)
		for _, obj := range hpaList.Items {
			if obj.GenerateName == "hvpa-1-" {
				num = num + 1
				hpa = obj.DeepCopy()
			}
		}
		if num == 1 {
			return nil
		}
		return fmt.Errorf("Error: Expected 1 HPA; found %v", len(hpaList.Items))
	}, timeout).Should(Succeed())

	// Delete the HPA and expect Reconcile to be called for HPA deletion
	Expect(k8sClient.Delete(context.TODO(), hpa)).NotTo(HaveOccurred())
	oldHpa := hpa.Name
	Eventually(func() error {
		num := 0
		k8sClient.List(context.TODO(), hpaList)
		for _, obj := range hpaList.Items {
			if obj.GenerateName == "hvpa-1-" {
				num = num + 1
				hpa = obj.DeepCopy()
			}
		}
		if num == 1 && hpa.Name != oldHpa {
			return nil
		}
		return fmt.Errorf("Error: Expected 1 new HPA; found %v", len(hpaList.Items))
	}, timeout).Should(Succeed())

	return hpa
}

func testVpaReconcile(k8sClient client.Client) *vpa_api.VerticalPodAutoscaler {
	vpaList := &vpa_api.VerticalPodAutoscalerList{}
	vpa := &vpa_api.VerticalPodAutoscaler{}
	Eventually(func() error {
		num := 0
		k8sClient.List(context.TODO(), vpaList)
		for _, obj := range vpaList.Items {
			if obj.GenerateName == "hvpa-1-" {
				num = num + 1
				vpa = obj.DeepCopy()
			}
		}
		if num == 1 {
			return nil
		}
		return fmt.Errorf("Error: Expected 1 VPA; found %v", len(vpaList.Items))
	}, timeout).Should(Succeed())
	return vpa
}

func testOverrideStabilization(k8sClient client.Client, deploytest *appsv1.Deployment, instance *hvpav1alpha2.Hvpa) {
	// Create a pod for the target deployment, and update status to "OOMKilled".
	// The field hvpa.status.overrideScaleUpStabilization should be set to true.
	p := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: deploytest.Namespace,
			Labels:    deploytest.Spec.Template.Labels,
		},
		Spec: v1.PodSpec{
			NodeName:   "test-node",
			Containers: deploytest.Spec.Template.Spec.Containers,
		},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:         deploytest.Spec.Template.Spec.Containers[0].Name,
					RestartCount: 2,
					LastTerminationState: v1.ContainerState{
						Terminated: &v1.ContainerStateTerminated{
							Reason:     "OOMKilled",
							FinishedAt: metav1.Now(),
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(context.TODO(), &p)).To(Succeed())
	Expect(k8sClient.Status().Update(context.TODO(), &p)).To(Succeed())

	Eventually(func() bool {
		h := &hvpav1alpha2.Hvpa{}
		k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, h)
		return h.Status.OverrideScaleUpStabilization
	}, timeout).Should(BeTrue())
}

func testScalingOnVPAReco(k8sClient client.Client, vpa *vpa_api.VerticalPodAutoscaler, instance *hvpav1alpha2.Hvpa, deploytest *appsv1.Deployment) {
	// Update VPA status, let HVPA scale
	hvpa := &hvpav1alpha2.Hvpa{}
	Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa)).To(Succeed())
	Expect(hvpa.Status.LastScaling.LastUpdated).To(BeNil())
	Eventually(func() error {
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: vpa.Name, Namespace: vpa.Namespace}, vpa); err != nil {
			return err
		}
		vpa.Status = *newVpaStatus(deploytest.Spec.Template.Spec.Containers[0].Name, "2G", "500m")
		return k8sClient.Update(context.TODO(), vpa)
	}, timeout).Should(Succeed())

	Eventually(func() error {
		hvpa = &hvpav1alpha2.Hvpa{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
			return err
		}
		if hvpa.Status.LastScaling.LastUpdated == nil {
			return fmt.Errorf("HVPA did not scale")
		}
		return nil
	}, timeout).Should(Succeed())
}

func testScalingOnHPAReco(k8sClient client.Client, hpa *autoscaling.HorizontalPodAutoscaler, instance *hvpav1alpha2.Hvpa, deploytest *appsv1.Deployment) {
	// Update HPA status, let HVPA scale
	hvpa := &hvpav1alpha2.Hvpa{}
	Eventually(func() error {
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
			return err
		}
		if hvpa.Status.LastScaling.LastUpdated == nil {
			return nil
		}
		return fmt.Errorf("hvpa status last update time not nil")
	}, timeout).Should(Succeed())

	Eventually(func() error {
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, hpa); err != nil {
			return err
		}
		hpa.Status = *newHpaStatus(*deploytest.Spec.Replicas, *deploytest.Spec.Replicas+2, nil)
		return k8sClient.Status().Update(context.TODO(), hpa)
	}, timeout).Should(Succeed())

	Eventually(func() error {
		hvpa = &hvpav1alpha2.Hvpa{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
			return err
		}
		if hvpa.Status.LastScaling.LastUpdated == nil {
			return fmt.Errorf("HVPA did not scale %+v", hvpa.Status)
		}
		return nil
	}, timeout).Should(Succeed())
}

func testNoScalingOnHvpaSpecUpdate(k8sClient client.Client, instance *hvpav1alpha2.Hvpa) {
	// Change hvpa spec without changing hpa and vpa status. hvpa recommendations should not change
	hvpa := &hvpav1alpha2.Hvpa{}
	Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa)).To(Succeed())
	lastScaling := hvpa.Status.LastScaling.DeepCopy()

	newScaleIntervals := []hvpav1alpha2.ScaleInterval{
		{
			MaxCPU:      resourcePtr("10"),
			MaxMemory:   resourcePtr("20G"),
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
	}
	Eventually(func() error {
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa); err != nil {
			return err
		}
		hvpa.Spec.ScaleIntervals = newScaleIntervals
		return k8sClient.Update(context.TODO(), hvpa)
	}, timeout).Should(Succeed())

	// Expect no change in scaling status after spec change, as HPA and VPA recommendations are not updated
	Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, hvpa)).To(Succeed())
	Expect(hvpa.Status.LastScaling.HpaStatus).To(Equal(lastScaling.HpaStatus))
	Expect(hvpa.Status.LastScaling.VpaStatus).To(Equal(lastScaling.VpaStatus))
}
