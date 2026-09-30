package operator

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

func TestDeploymentProgressRequiresEveryDesiredReplicaAndActiveRollout(t *testing.T) {
	replicas := int32(2)
	complete := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
		},
	}
	complete.Generation = 2
	if !deploymentProgressed(complete) {
		t.Fatal("complete deployment was not progressed")
	}
	for name, mutate := range map[string]func(*appsv1.Deployment){
		"paused":           func(d *appsv1.Deployment) { d.Spec.Paused = true },
		"old replica":      func(d *appsv1.Deployment) { d.Status.UpdatedReplicas = 1 },
		"surge remains":    func(d *appsv1.Deployment) { d.Status.Replicas = 3 },
		"not all ready":    func(d *appsv1.Deployment) { d.Status.AvailableReplicas = 1 },
		"not observed yet": func(d *appsv1.Deployment) { d.Status.ObservedGeneration = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := complete.DeepCopy()
			mutate(candidate)
			if deploymentProgressed(candidate) {
				t.Fatal("incomplete deployment was progressed")
			}
		})
	}
}

func TestStatefulAndDaemonProgressRequireUpdatedReadyReplicas(t *testing.T) {
	replicas := int32(2)
	stateful := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2,
			CurrentRevision: "revision-two", UpdateRevision: "revision-two",
		},
	}
	stateful.Generation = 2
	if !statefulSetProgressed(stateful) {
		t.Fatal("complete stateful set was not progressed")
	}
	stateful.Status.ReadyReplicas = 1
	if statefulSetProgressed(stateful) {
		t.Fatal("unready stateful set was progressed")
	}

	daemon := &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{
		ObservedGeneration: 2, DesiredNumberScheduled: 2, UpdatedNumberScheduled: 2,
		NumberAvailable: 2,
	}}
	daemon.Generation = 2
	if !daemonSetProgressed(daemon) {
		t.Fatal("complete daemon set was not progressed")
	}
	daemon.Status.UpdatedNumberScheduled = 1
	if daemonSetProgressed(daemon) {
		t.Fatal("daemon set with an old pod was progressed")
	}
}
