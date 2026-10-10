package operator

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func TestWorkloadPatchRechecksStateAfterConflict(t *testing.T) {
	const stamp = "v1:new-stamp"
	annKey := hikyov1.StampAnnotationPrefix + testTarget
	kinds := []struct {
		name string
		make func() client.Object
	}{
		{"Deployment", func() client.Object { return makeOptedInDeployment("web", testTarget) }},
		{"StatefulSet", func() client.Object { return makeOptedInStatefulSet("web", testTarget) }},
		{"DaemonSet", func() client.Object { return makeOptedInDaemonSet("web", testTarget) }},
	}
	cases := []struct {
		name         string
		wantAttempts int
		wantError    bool
	}{
		{"foreign_annotation", 2, false},
		{"revoked_consent", 1, true},
		{"replacement", 1, true},
		{"already_stamped", 1, false},
		{"persistent_conflict", retry.DefaultRetry.Steps, true},
		{"permanent_failure", 1, true},
		{"request_deadline", 1, true},
		{"request_canceled", 1, true},
	}
	for _, kind := range kinds {
		for _, tc := range cases {
			t.Run(kind.name+"/"+tc.name, func(t *testing.T) {
				obj := kind.make()
				attempts := 0
				h := newHarness(t, interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					attempts++
					if tc.name == "permanent_failure" {
						return errors.New("permanent patch failure")
					}
					if tc.name == "request_deadline" {
						return context.DeadlineExceeded
					}
					if tc.name == "request_canceled" {
						return context.Canceled
					}
					if attempts != 1 && tc.name != "persistent_conflict" {
						return c.Patch(ctx, obj, patch, opts...)
					}
					fresh := kind.make()
					if err := c.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
						return err
					}
					switch tc.name {
					case "foreign_annotation":
						setWorkloadTemplateAnnotations(fresh, map[string]string{"example.com/foreign": "preserve"})
					case "revoked_consent":
						fresh.SetAnnotations(nil)
					case "replacement":
						if err := c.Delete(ctx, fresh); err != nil {
							return err
						}
						fresh.SetUID("replacement-uid")
						fresh.SetResourceVersion("")
						if err := c.Create(ctx, fresh); err != nil {
							return err
						}
					case "already_stamped":
						setWorkloadTemplateAnnotations(fresh, map[string]string{annKey: stamp})
					}
					if tc.name != "replacement" {
						if err := c.Update(ctx, fresh); err != nil {
							return err
						}
					}
					return apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "workloads"}, obj.GetName(), errors.New("concurrent write"))
				}}, obj)
				err := h.r.patchPodTemplateAnnotation(t.Context(), obj, annKey, stamp)
				if (err != nil) != tc.wantError {
					t.Fatalf("patch error = %v, want error = %v", err, tc.wantError)
				}
				if tc.name == "request_deadline" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("request deadline lost: %v", err)
				}
				if tc.name == "request_canceled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("request cancellation lost: %v", err)
				}
				if tc.name == "persistent_conflict" && !apierrors.IsConflict(err) {
					t.Fatalf("exhausted conflict lost: %v", err)
				}
				if attempts != tc.wantAttempts {
					t.Fatalf("patch attempts = %d, want %d", attempts, tc.wantAttempts)
				}
				got := kind.make()
				if err := h.cl.Get(t.Context(), client.ObjectKeyFromObject(obj), got); err != nil {
					t.Fatal(err)
				}
				annotations := podTemplateAnnotations(got)
				if tc.wantError && annotations[annKey] != "" {
					t.Fatalf("failed patch stamped workload: %v", annotations)
				}
				if !tc.wantError && annotations[annKey] != stamp {
					t.Fatalf("successful patch omitted stamp: %v", annotations)
				}
				if tc.name == "foreign_annotation" && annotations["example.com/foreign"] != "preserve" {
					t.Fatalf("retry lost concurrent template annotation: %v", annotations)
				}
			})
		}
	}
}

func setWorkloadTemplateAnnotations(obj client.Object, annotations map[string]string) {
	switch workload := obj.(type) {
	case *appsv1.Deployment:
		workload.Spec.Template.Annotations = annotations
	case *appsv1.StatefulSet:
		workload.Spec.Template.Annotations = annotations
	case *appsv1.DaemonSet:
		workload.Spec.Template.Annotations = annotations
	}
}
