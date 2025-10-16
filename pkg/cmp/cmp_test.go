package cmp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
)

type sliceEqualUnorderedTest struct {
	want bool
	a    []string
	b    []string
}

var _ = DescribeTable("SliceEqualUnordered",
	func(t *sliceEqualUnorderedTest) {
		cmp := func(x, y string) bool { return x == y }
		Expect(SliceEqualUnordered[string](t.a, t.b, cmp)).To(Equal(t.want))
	},
	Entry("empty slices", &sliceEqualUnorderedTest{
		want: true,
		a:    []string{},
		b:    []string{},
	}),
	Entry("nils", &sliceEqualUnorderedTest{
		want: true,
		a:    nil,
		b:    nil,
	}),
	Entry("empty vs nil", &sliceEqualUnorderedTest{
		want: true,
		a:    []string{},
		b:    nil,
	}),
	Entry("elements don't match", &sliceEqualUnorderedTest{
		want: false,
		a:    []string{"c"},
		b:    []string{"d"},
	}),
	Entry("length 2 out-of-order", &sliceEqualUnorderedTest{
		want: true,
		a:    []string{"c", "d"},
		b:    []string{"d", "c"},
	}),
	Entry("multiple matches", &sliceEqualUnorderedTest{
		want: true,
		a:    []string{"c", "c", "c", "d", "d", "d"},
		b:    []string{"c", "d", "c", "d", "c", "d"},
	}),
	Entry("extra in a", &sliceEqualUnorderedTest{
		want: false,
		a:    []string{"c", "d", "e"},
		b:    []string{"c", "d"},
	}),
	Entry("same length not enough available in c's in b", &sliceEqualUnorderedTest{
		want: false,
		a:    []string{"c", "c"},
		b:    []string{"c", "d"},
	}),
	Entry("extra in b", &sliceEqualUnorderedTest{
		want: false,
		a:    []string{"c", "d"},
		b:    []string{"c", "d", "e"},
	}),
)

type sliceEqualTest struct {
	want bool
	a    []string
	b    []string
}

var _ = DescribeTable("SliceEqual",
	func(t *sliceEqualTest) {
		Expect(SliceEqual(t.a, t.b)).To(Equal(t.want))
	},
	Entry("nil slices", &sliceEqualTest{
		want: true,
		a:    nil,
		b:    nil,
	}),
	Entry("nil slice vs empty slice", &sliceEqualTest{
		want: true,
		a:    nil,
		b:    []string{},
	}),
	Entry("matches", &sliceEqualTest{
		want: true,
		a:    []string{"c", "d"},
		b:    []string{"c", "d"},
	}),
	Entry("out of order", &sliceEqualTest{
		want: false,
		a:    []string{"c", "d"},
		b:    []string{"d", "c"},
	}),
	Entry("unequal length", &sliceEqualTest{
		want: false,
		a:    []string{"c", "d", "e"},
		b:    []string{"c", "d"},
	}),
)

type ptrValEqualTest struct {
	want bool
	a    *string
	b    *string
}

var _ = DescribeTable("PtrValEqual",
	func(t *ptrValEqualTest) {
		Expect(PtrValEqual(t.a, t.b)).To(Equal(t.want))
	},
	Entry("nils", &ptrValEqualTest{
		want: true,
		a:    nil,
		b:    nil,
	}),
	Entry("nil vs value", &ptrValEqualTest{
		want: false,
		a:    nil,
		b:    utils.Ptr(""),
	}),
	Entry("equal values", &ptrValEqualTest{
		want: true,
		a:    utils.Ptr("c"),
		b:    utils.Ptr("c"),
	}),
	Entry("unequal values", &ptrValEqualTest{
		want: false,
		a:    utils.Ptr("c"),
		b:    utils.Ptr("d"),
	}),
)

type ptrValEqualFnTest struct {
	want bool
	a    *string
	b    *string
}

var _ = DescribeTable("PtrValEqualFn",
	func(t *ptrValEqualFnTest) {
		cmp := func(x, y string) bool { return x == y }
		Expect(PtrValEqualFn(t.a, t.b, cmp)).To(Equal(t.want))
	},
	Entry("nils", &ptrValEqualFnTest{
		want: true,
		a:    nil,
		b:    nil,
	}),
	Entry("nil vs value", &ptrValEqualFnTest{
		want: false,
		a:    nil,
		b:    utils.Ptr(""),
	}),
	Entry("equal values", &ptrValEqualFnTest{
		want: true,
		a:    utils.Ptr("c"),
		b:    utils.Ptr("c"),
	}),
	Entry("unequal values", &ptrValEqualFnTest{
		want: false,
		a:    utils.Ptr("c"),
		b:    utils.Ptr("d"),
	}),
)

type lenSlicePtrTest struct {
	want int
	in   *[]any
}

var _ = DescribeTable("LenSlicePtr",
	func(t *lenSlicePtrTest) {
		Expect(LenSlicePtr(t.in)).To(Equal(t.want))
	},
	Entry("nil pointer", &lenSlicePtrTest{
		want: 0,
		in:   nil,
	}),
	Entry("nil slice", &lenSlicePtrTest{
		want: 0,
		in:   utils.Ptr[[]any](nil),
	}),
	Entry("empty slice", &lenSlicePtrTest{
		want: 0,
		in:   utils.Ptr[[]any]([]any{}),
	}),
	Entry("length 1 slice", &lenSlicePtrTest{
		want: 1,
		in:   utils.Ptr[[]any]([]any{nil}),
	}),
)

var _ = Describe("UnpackPtr", func() {
	It("should return zero value for nil", func() {
		Expect(UnpackPtr[string](nil)).To(Equal(""))
	})

	It("should return zero value for zero value", func() {
		var zero string
		Expect(UnpackPtr(&zero)).To(Equal(""))
	})

	It("should", func() {
		Expect(UnpackPtr(utils.Ptr("hello"))).To(Equal("hello"))
	})
})
