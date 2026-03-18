package observability

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/client-go/tools/record"
)

func EmitConditionTransitions(
	recorder record.EventRecorder,
	obj client.Object,
	oldConditions []metav1.Condition,
	newConditions []metav1.Condition,
	conditionTypes ...string,
) {
	if recorder == nil || obj == nil {
		return
	}

	for _, conditionType := range conditionTypes {
		oldCondition := meta.FindStatusCondition(oldConditions, conditionType)
		newCondition := meta.FindStatusCondition(newConditions, conditionType)
		if sameCondition(oldCondition, newCondition) || newCondition == nil {
			continue
		}

		eventType := corev1.EventTypeNormal
		if newCondition.Status != metav1.ConditionTrue {
			eventType = corev1.EventTypeWarning
		}

		recorder.Eventf(obj, eventType, newCondition.Reason, "%s: %s", newCondition.Type, newCondition.Message)
	}
}

func sameCondition(a, b *metav1.Condition) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}

	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}
