package testutil

import (
	"context"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DeleteAndWaitForKubernetesResource(ctx context.Context, cl client.Client, obj client.Object) {
	GinkgoHelper()
	Expect(cl.Delete(ctx, obj)).To(Succeed())
	Eventually(func(g Gomega, ctx context.Context) {
		g.Expect(cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)).Should(WithTransform(apierrors.IsNotFound, BeTrue()), "Expected resource %s to eventually be deleted", client.ObjectKeyFromObject(obj))

	}).WithContext(ctx).Should(Succeed())
}

func HaveAtomicValue[T any](matcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(func(a *atomic.Pointer[T]) *T {
		t := a.Load()
		return t
	}, matcher)
}
