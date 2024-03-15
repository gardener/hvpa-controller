// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0
package utils_test

import (
	"fmt"
	"time"

	. "github.com/gardener/hvpa-controller/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
)

var (
	cet, _    = time.LoadLocation("CET")
	cetOffset = 2
)

var _ = Describe("utils", func() {
	Context("MaintenanceTime", func() {
		DescribeTable("#NewMaintenanceTime",
			func(hour, minute, second int, panics bool) {
				if !panics {
					mt := NewMaintenanceTime(hour, minute, second)
					Expect(mt.Hour()).To(Equal(hour))
					Expect(mt.Minute()).To(Equal(minute))
					Expect(mt.Second()).To(Equal(second))
				} else {
					Expect(func() { NewMaintenanceTime(hour, minute, second) }).To(Panic())
				}
			},

			Entry("valid values", 16, 5, 54, false),
			Entry("invalid value for hour", 25, 5, 54, true),
			Entry("invalid value for minute", 16, 72, 54, true),
			Entry("invalid value for second", 16, 5, 97, true),
		)

		Describe("#ParseMaintenanceTime", func() {
			It("should return the correctly parsed maintenance time", func() {
				var (
					hour   = 16
					minute = 5
					second = 54
					value  = fmt.Sprintf("%.02d%.02d%.02d+%.02d00", hour, minute, second, cetOffset)
				)

				mt, err := ParseMaintenanceTime(value)

				Expect(err).NotTo(HaveOccurred())
				Expect(mt).To(Equal(NewMaintenanceTime(hour-cetOffset, minute, second)))
			})
		})

		var (
			hour            = 16
			minute          = 15
			second          = 23
			maintenanceTime = NewMaintenanceTime(hour, minute, second)
		)

		Describe("#String", func() {
			It("should return the correct string representation", func() {
				Expect(maintenanceTime.String()).To(Equal(fmt.Sprintf("%.02d:%.02d:%.02d", hour, minute, second)))
			})
		})

		Describe("#Formatted", func() {
			It("should return the time in the correct time layout format", func() {
				Expect(maintenanceTime.Formatted()).To(Equal(fmt.Sprintf("%.02d%.02d%.02d+0000", hour, minute, second)))
			})
		})

		Describe("#Hour", func() {
			It("should return the correct hour", func() {
				Expect(maintenanceTime.Hour()).To(Equal(hour))
			})
		})

		Describe("#Minute", func() {
			It("should return the correct minute", func() {
				Expect(maintenanceTime.Minute()).To(Equal(minute))
			})
		})

		Describe("#Second", func() {
			It("should return the correct second", func() {
				Expect(maintenanceTime.Second()).To(Equal(second))
			})
		})

		Describe("#Add", func() {
			It("should return the correct second", func() {
				Expect(maintenanceTime.Add(14, 14, 14)).To(Equal(NewMaintenanceTime(6, 29, 37)))
			})
		})

		DescribeTable("#Compare",
			func(m1, m2 *MaintenanceTime, matcher gomegatypes.GomegaMatcher) {
				Expect(m1.Compare(m2)).To(matcher)
			},

			Entry("smaller hour", NewMaintenanceTime(1, 0, 0), NewMaintenanceTime(2, 0, 0), BeNumerically("<", 0)),
			Entry("same hour, smaller minute", NewMaintenanceTime(1, 0, 0), NewMaintenanceTime(1, 1, 0), BeNumerically("<", 0)),
			Entry("same hour, same minute, smaller second", NewMaintenanceTime(1, 0, 0), NewMaintenanceTime(1, 0, 1), BeNumerically("<", 0)),
			Entry("same hour, same minute, same second", NewMaintenanceTime(1, 0, 0), NewMaintenanceTime(1, 0, 0), BeNumerically("==", 0)),
			Entry("same hour, same minute, greater second", NewMaintenanceTime(1, 0, 1), NewMaintenanceTime(1, 0, 0), BeNumerically(">", 0)),
			Entry("same hour, greater minute", NewMaintenanceTime(1, 1, 0), NewMaintenanceTime(1, 0, 0), BeNumerically(">", 0)),
			Entry("greater hour", NewMaintenanceTime(2, 0, 0), NewMaintenanceTime(1, 0, 0), BeNumerically(">", 0)),
		)
	})

	Context("MaintenanceTimeWindow", func() {
		Describe("#NewMaintenanceTimeWindow", func() {
			It("should return a maintenance time window with correct begin and end", func() {
				var (
					begin = NewMaintenanceTime(1, 2, 3)
					end   = NewMaintenanceTime(4, 5, 6)
				)

				tw := NewMaintenanceTimeWindow(begin, end)

				Expect(tw.Begin()).To(Equal(begin))
				Expect(tw.End()).To(Equal(end))
			})
		})

		var (
			begin                 = NewMaintenanceTime(1, 1, 1)
			end                   = NewMaintenanceTime(1, 1, 1)
			maintenanceTimeWindow = NewMaintenanceTimeWindow(begin, end)
		)

		DescribeTable("#ParseMaintenanceTimeWindow",
			func(begin, end string, errorMatcher, timeWindowMatcher gomegatypes.GomegaMatcher) {
				timeWindow, err := ParseMaintenanceTimeWindow(begin, end)

				Expect(err).To(errorMatcher)
				Expect(timeWindow).To(timeWindowMatcher)
			},

			Entry("invalid begin", "foo", end.Formatted(), HaveOccurred(), BeNil()),
			Entry("invalid end", begin.Formatted(), "foo", HaveOccurred(), BeNil()),
			Entry("valid maintenance time window", begin.Formatted(), end.Formatted(), Not(HaveOccurred()), Equal(maintenanceTimeWindow)),
		)

		Describe("#String", func() {
			It("should return the correct string representation", func() {
				Expect(maintenanceTimeWindow.String()).To(Equal(fmt.Sprintf("begin=%s, end=%s", begin, end)))
			})
		})

		Describe("#Begin", func() {
			It("should return the correct begin", func() {
				Expect(maintenanceTimeWindow.Begin()).To(Equal(begin))
			})
		})

		Describe("#End", func() {
			It("should return the correct end", func() {
				Expect(maintenanceTimeWindow.End()).To(Equal(end))
			})
		})

		Describe("#WithBegin", func() {
			It("should return the new maintenance time window", func() {
				newBegin := NewMaintenanceTime(4, 4, 4)
				Expect(maintenanceTimeWindow.WithBegin(newBegin)).To(Equal(NewMaintenanceTimeWindow(newBegin, end)))
			})
		})

		Describe("#WithEnd", func() {
			It("should return the new maintenance time window", func() {
				newEnd := NewMaintenanceTime(4, 4, 4)
				Expect(maintenanceTimeWindow.WithEnd(newEnd)).To(Equal(NewMaintenanceTimeWindow(begin, newEnd)))
			})
		})

		var (
			time0  = NewMaintenanceTime(0, 0, 0)
			time1  = NewMaintenanceTime(1, 0, 0)
			time16 = NewMaintenanceTime(16, 0, 0)
			time19 = NewMaintenanceTime(19, 0, 0)
			time23 = NewMaintenanceTime(23, 0, 0)

			from16to19 = NewMaintenanceTimeWindow(time16, time19)
			from0to1   = NewMaintenanceTimeWindow(time0, time1)
			from23to1  = NewMaintenanceTimeWindow(time23, time1)
			from23to0  = NewMaintenanceTimeWindow(time23, time0)
		)

		DescribeTable("#Contains",
			func(maintenanceTimeWindow *MaintenanceTimeWindow, checkedTime time.Time, withinTimeWindow bool) {
				Expect(maintenanceTimeWindow.Contains(checkedTime)).To(Equal(withinTimeWindow), "checkedTime=%s maintenanceTimeWindow=%s", checkedTime, maintenanceTimeWindow)
			},

			Entry("begin and end on the same day (16-19)", from16to19, newTime(15, 59, 59, 9999), false),
			Entry("begin and end on the same day (16-19)", from16to19, newTime(19, 1, 0, 0), false),
			Entry("begin and end on the same day (16-19)", from16to19, newTime(16, 0, 0, 0), true),
			Entry("begin and end on the same day (16-19)", from16to19, newTime(19, 0, 0, 0), true),
			Entry("begin and end on the same day (16-19)", from16to19, newTime(17, 0, 0, 0), true),

			Entry("begin and end on the same day (0-1)", from0to1, newTime(23, 59, 59, 9999), false),
			Entry("begin and end on the same day (0-1)", from0to1, newTime(2, 0, 0, 0), false),
			Entry("begin and end on the same day (0-1)", from0to1, newTime(0, 0, 0, 0), true),
			Entry("begin and end on the same day (0-1)", from0to1, newTime(1, 0, 0, 0), true),
			Entry("begin and end on the same day (0-1)", from0to1, newTime(0, 30, 0, 0), true),

			Entry("begin and end on different day (23-1)", from23to1, newTime(22, 59, 59, 9999), false),
			Entry("begin and end on different day (23-1)", from23to1, newTime(2, 0, 0, 0), false),
			Entry("begin and end on different day (23-1)", from23to1, newTime(23, 0, 0, 0), true),
			Entry("begin and end on different day (23-1)", from23to1, newTime(1, 0, 0, 0), true),
			Entry("begin and end on different day (23-1)", from23to1, newTime(0, 59, 0, 0), true),

			Entry("begin and end on different day (23-0)", from23to0, newTime(22, 59, 59, 9999), false),
			Entry("begin and end on different day (23-0)", from23to0, newTime(1, 0, 0, 0), false),
			Entry("begin and end on different day (23-0)", from23to0, newTime(23, 0, 0, 0), true),
			Entry("begin and end on different day (23-0)", from23to0, newTime(0, 0, 0, 0), true),
			Entry("begin and end on different day (23-0)", from23to0, newTime(23, 45, 0, 0), true),
		)

		DescribeTable("#Duration",
			func(maintenanceTimeWindow *MaintenanceTimeWindow, expected time.Duration) {
				Expect(maintenanceTimeWindow.Duration()).To(Equal(expected))
			},

			Entry("begin and end on the same day (16-19)", from16to19, 3*time.Hour),
			Entry("begin and end on the same day (0-1)", from0to1, 1*time.Hour),
			Entry("begin and end on different day (23-1)", from23to1, 2*time.Hour),
			Entry("begin and end on different day (23-0)", from23to0, 1*time.Hour),
		)
	})
})

func newTime(hour, minute, second, nanosecond int) time.Time {
	return time.Date(1, time.January, 1, hour, minute, second, nanosecond, time.UTC)
}
