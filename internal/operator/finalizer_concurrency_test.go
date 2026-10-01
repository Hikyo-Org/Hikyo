package operator

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func TestFinalizerPatchUsesFreshForeignFinalizers(t *testing.T) {
	for _, add := range []bool{false, true} {
		t.Run(map[bool]string{true: "add", false: "remove"}[add], func(t *testing.T) {
			cr := makeCR("app")
			cr.Finalizers = []string{"example.com/stale"}
			if !add {
				cr.Finalizers = append(cr.Finalizers, hikyov1.OrphanFinalizer)
			}
			h := newHarness(t, interceptor.Funcs{}, cr)
			stale := h.getCR("app")
			current := h.getCR("app")
			current.Finalizers = []string{"example.com/fresh"}
			if !add {
				current.Finalizers = append(current.Finalizers, hikyov1.OrphanFinalizer)
			}
			if err := h.cl.Update(t.Context(), current); err != nil {
				t.Fatal(err)
			}
			mutate := controllerutil.RemoveFinalizer
			if add {
				mutate = controllerutil.AddFinalizer
			}
			if err := h.r.patchFinalizers(t.Context(), stale, mutate); err != nil {
				t.Fatal(err)
			}
			got := h.getCR("app")
			if !slices.Contains(got.Finalizers, "example.com/fresh") || slices.Contains(got.Finalizers, "example.com/stale") || controllerutil.ContainsFinalizer(got, hikyov1.OrphanFinalizer) != add {
				t.Fatalf("fresh finalizer list not preserved: %v", got.Finalizers)
			}
		})
	}
}

func TestFinalizerPatchConflictsOnInterveningWriter(t *testing.T) {
	cr := makeCR("app")
	cr.Finalizers = []string{"example.com/initial"}
	raced := false
	h := newHarness(t, interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if _, ok := obj.(*hikyov1.HikyoSecret); !ok {
			return c.Patch(ctx, obj, patch, opts...)
		}
		data, err := patch.Data(obj)
		if err != nil {
			return err
		}
		var payload struct {
			Metadata struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		if payload.Metadata.ResourceVersion == "" {
			t.Error("finalizer merge patch omitted the resourceVersion precondition")
		}
		var current hikyov1.HikyoSecret
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), &current); err != nil {
			return err
		}
		current.Finalizers = append(current.Finalizers, "example.com/racing")
		if err := c.Update(ctx, &current); err != nil {
			return err
		}
		raced = true
		return c.Patch(ctx, obj, patch, opts...)
	}}, cr)
	if err := h.r.patchFinalizers(t.Context(), h.getCR("app"), controllerutil.AddFinalizer); !apierrors.IsConflict(err) {
		t.Fatalf("intervening writer error=%v, want conflict", err)
	}
	got := h.getCR("app")
	if !raced || !slices.Contains(got.Finalizers, "example.com/racing") || controllerutil.ContainsFinalizer(got, hikyov1.OrphanFinalizer) {
		t.Fatalf("conflicting patch changed finalizers: %v", got.Finalizers)
	}
}

func TestFinalizerPatchRefusesRecreatedCR(t *testing.T) {
	cr := makeCR("app")
	h := newHarness(t, interceptor.Funcs{}, cr)
	stale := h.getCR("app")
	stale.UID = "old-deleted-cr"
	if err := h.r.patchFinalizers(t.Context(), stale, controllerutil.AddFinalizer); err == nil {
		t.Fatal("finalizer patched a replacement CR with a different UID")
	}
	if got := h.getCR("app"); len(got.Finalizers) != 0 {
		t.Fatalf("replacement CR finalizers changed: %v", got.Finalizers)
	}
}
