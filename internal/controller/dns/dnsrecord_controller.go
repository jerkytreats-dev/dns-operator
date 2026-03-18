package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/jerkytreats/dns-operator/api/common"
	dnsv1alpha1 "github.com/jerkytreats/dns-operator/api/dns/v1alpha1"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	dnsdomain "github.com/jerkytreats/dns-operator/internal/dns"
	"github.com/jerkytreats/dns-operator/internal/observability"
	publishdomain "github.com/jerkytreats/dns-operator/internal/publish"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DNSRecordReconciler keeps the authoritative DNS zone artifact in sync.
type DNSRecordReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=dns.jerkytreats.dev,resources=dnsrecords,verbs=get;list;watch
// +kubebuilder:rbac:groups=dns.jerkytreats.dev,resources=dnsrecords/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=publish.jerkytreats.dev,resources=publishedservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=publish.jerkytreats.dev,resources=publishedservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *DNSRecordReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	started := time.Now()
	defer func() {
		observability.ObserveReconcile("dns-dnsrecord", started, result, err)
	}()

	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)
	if req.Namespace == "" {
		return ctrl.Result{}, nil
	}

	var records dnsv1alpha1.DNSRecordList
	if err = r.List(ctx, &records, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list dns records: %w", err)
	}

	var services publishv1alpha1.PublishedServiceList
	if err = r.List(ctx, &services, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list published services: %w", err)
	}

	projected := make([]dnsdomain.AuthoritativeRecord, 0, len(records.Items)+len(services.Items))
	recordErrors := map[types.NamespacedName]error{}
	serviceErrors := map[types.NamespacedName]error{}
	publishRuntimeTarget, publishRuntimeTargetErr := r.resolvePublishRuntimeTarget(ctx, req.Namespace)

	for i := range records.Items {
		record := records.Items[i]
		projectedRecord, err := dnsdomain.RecordForDNSRecord(record)
		if err != nil {
			recordErrors[client.ObjectKeyFromObject(&record)] = err
			continue
		}
		projected = append(projected, projectedRecord)
	}

	for i := range services.Items {
		service := services.Items[i]
		projectedRecord, err := r.projectPublishedServiceRecord(service, publishRuntimeTarget, publishRuntimeTargetErr)
		if err != nil {
			serviceErrors[client.ObjectKeyFromObject(&service)] = err
			continue
		}
		projected = append(projected, projectedRecord)
	}

	rendered := dnsdomain.RenderZone(projected)
	operation, configMapErr := r.reconcileZoneConfigMap(ctx, req.Namespace, rendered)
	observability.RecordArtifactUpdate("dns-dnsrecord", "zone_configmap", operation)
	if configMapErr != nil {
		logger.Error(configMapErr, "unable to reconcile zone configmap")
	}

	for i := range records.Items {
		record := records.Items[i]
		key := client.ObjectKeyFromObject(&record)
		if err := r.updateDNSRecordStatus(ctx, &record, rendered, recordErrors[key], configMapErr); err != nil {
			return ctrl.Result{}, err
		}
	}

	for i := range services.Items {
		service := services.Items[i]
		key := client.ObjectKeyFromObject(&service)
		if err := r.updatePublishedServiceStatus(ctx, &service, serviceErrors[key], configMapErr); err != nil {
			return ctrl.Result{}, err
		}
	}

	if configMapErr != nil {
		return ctrl.Result{}, configMapErr
	}

	return ctrl.Result{}, nil
}

func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager) error {
	dnsRecordChanged := predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			if updateEvent.ObjectOld == nil || updateEvent.ObjectNew == nil {
				return true
			}
			return updateEvent.ObjectOld.GetGeneration() != updateEvent.ObjectNew.GetGeneration()
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return true },
	}

	publishedServiceChanged := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return true },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsv1alpha1.DNSRecord{}, builder.WithPredicates(dnsRecordChanged)).
		Watches(
			&publishv1alpha1.PublishedService{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      dnsdomain.ZoneSyncRequestName,
					},
				}}
			}),
			builder.WithPredicates(publishedServiceChanged),
		).
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				if obj.GetName() != publishdomain.RuntimeServiceName {
					return nil
				}
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      dnsdomain.ZoneSyncRequestName,
					},
				}}
			}),
			builder.WithPredicates(publishedServiceChanged),
		).
		Named("dns-dnsrecord").
		Complete(r)
}

func (r *DNSRecordReconciler) projectPublishedServiceRecord(
	service publishv1alpha1.PublishedService,
	publishRuntimeTarget string,
	publishRuntimeTargetErr error,
) (dnsdomain.AuthoritativeRecord, error) {
	if service.Spec.PublishMode != publishv1alpha1.PublishModeHTTPSProxy {
		return dnsdomain.RecordForPublishedService(service)
	}
	if publishRuntimeTargetErr != nil {
		return dnsdomain.AuthoritativeRecord{}, publishRuntimeTargetErr
	}
	return dnsdomain.RecordForPublishedServiceTarget(service, publishRuntimeTarget)
}

func (r *DNSRecordReconciler) resolvePublishRuntimeTarget(ctx context.Context, namespace string) (string, error) {
	var runtimeService corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: publishdomain.RuntimeServiceName, Namespace: namespace}, &runtimeService); err != nil {
		return "", fmt.Errorf("get publish runtime service: %w", err)
	}
	for _, ingress := range runtimeService.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP, nil
		}
		if ingress.Hostname != "" {
			return ingress.Hostname, nil
		}
	}
	if runtimeService.Spec.ClusterIP != "" && runtimeService.Spec.ClusterIP != corev1.ClusterIPNone {
		return runtimeService.Spec.ClusterIP, nil
	}
	return "", fmt.Errorf("publish runtime service does not have an address yet")
}

func (r *DNSRecordReconciler) reconcileZoneConfigMap(ctx context.Context, namespace string, rendered dnsdomain.RenderedZone) (string, error) {
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rendered.ConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dns-operator",
				"dns.jerkytreats.dev/managed": "true",
			},
			Annotations: map[string]string{
				"dns.jerkytreats.dev/hash": rendered.Hash,
				"dns.jerkytreats.dev/zone": "internal.example.test",
			},
		},
		Data: map[string]string{
			rendered.DataKey: rendered.Content,
		},
	}

	current := &corev1.ConfigMap{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return observability.OperationCreate, r.Create(ctx, desired)
		}
		return "", err
	}

	changed := false
	if current.Data[rendered.DataKey] != rendered.Content {
		current.Data = desired.Data
		changed = true
	}

	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	if current.Annotations == nil {
		current.Annotations = map[string]string{}
	}

	for key, value := range desired.Labels {
		if current.Labels[key] != value {
			current.Labels[key] = value
			changed = true
		}
	}
	for key, value := range desired.Annotations {
		if current.Annotations[key] != value {
			current.Annotations[key] = value
			changed = true
		}
	}

	if !changed {
		return observability.OperationNoop, nil
	}

	return observability.OperationUpdate, r.Update(ctx, current)
}

func (r *DNSRecordReconciler) updateDNSRecordStatus(
	ctx context.Context,
	record *dnsv1alpha1.DNSRecord,
	rendered dnsdomain.RenderedZone,
	projectionErr error,
	configMapErr error,
) error {
	base := record.DeepCopy()
	record.Status.ObservedGeneration = record.Generation
	record.Status.ZoneConfigMapName = rendered.ConfigMapName
	record.Status.RenderedValues = append([]string(nil), record.Spec.Values...)
	record.Status.Conditions = resetConditions(record.Status.Conditions)

	if projectionErr != nil {
		setFalseCondition(&record.Status.Conditions, common.ConditionInputValid, "ValidationFailed", projectionErr.Error(), record.Generation)
		setFalseCondition(&record.Status.Conditions, common.ConditionAccepted, "Rejected", projectionErr.Error(), record.Generation)
		setFalseCondition(&record.Status.Conditions, common.ConditionReady, "ProjectionFailed", projectionErr.Error(), record.Generation)
	} else if configMapErr != nil {
		setTrueCondition(&record.Status.Conditions, common.ConditionInputValid, "Validated", "record accepted for rendering", record.Generation)
		setTrueCondition(&record.Status.Conditions, common.ConditionAccepted, "Accepted", "record accepted for rendering", record.Generation)
		setFalseCondition(&record.Status.Conditions, common.ConditionReady, "ConfigMapUpdateFailed", configMapErr.Error(), record.Generation)
	} else {
		setTrueCondition(&record.Status.Conditions, common.ConditionInputValid, "Validated", "record accepted for rendering", record.Generation)
		setTrueCondition(&record.Status.Conditions, common.ConditionAccepted, "Accepted", "record accepted for rendering", record.Generation)
		setTrueCondition(&record.Status.Conditions, common.ConditionReady, "Rendered", "record rendered into authoritative zone output", record.Generation)
	}

	if equalDNSRecordStatus(base.Status, record.Status) {
		return nil
	}

	if err := r.Status().Patch(ctx, record, client.MergeFrom(base)); err != nil {
		return err
	}
	observability.EmitConditionTransitions(
		r.Recorder,
		record,
		base.Status.Conditions,
		record.Status.Conditions,
		common.ConditionInputValid,
		common.ConditionAccepted,
		common.ConditionReady,
	)
	return nil
}

func (r *DNSRecordReconciler) updatePublishedServiceStatus(
	ctx context.Context,
	service *publishv1alpha1.PublishedService,
	projectionErr error,
	configMapErr error,
) error {
	base := service.DeepCopy()
	service.Status.ObservedGeneration = service.Generation
	service.Status.Hostname = service.Spec.Hostname
	if service.Spec.PublishMode == publishv1alpha1.PublishModeHTTPSProxy {
		service.Status.URL = "https://" + service.Spec.Hostname
	} else {
		service.Status.URL = ""
	}
	service.Status.Conditions = resetConditions(service.Status.Conditions)

	if projectionErr != nil {
		setFalseCondition(&service.Status.Conditions, common.ConditionDNSReady, "ProjectionFailed", projectionErr.Error(), service.Generation)
	} else if configMapErr != nil {
		setFalseCondition(&service.Status.Conditions, common.ConditionDNSReady, "ConfigMapUpdateFailed", configMapErr.Error(), service.Generation)
	} else {
		setTrueCondition(&service.Status.Conditions, common.ConditionDNSReady, "Rendered", "service hostname rendered into authoritative zone output", service.Generation)
	}

	if equalPublishedServiceStatus(base.Status, service.Status) {
		return nil
	}

	if err := r.Status().Patch(ctx, service, client.MergeFrom(base)); err != nil {
		return err
	}
	observability.EmitConditionTransitions(
		r.Recorder,
		service,
		base.Status.Conditions,
		service.Status.Conditions,
		common.ConditionDNSReady,
	)
	return nil
}

func resetConditions(conditions []metav1.Condition) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(conditions))
	seen := sets.New[string]()
	for _, condition := range conditions {
		if seen.Has(condition.Type) {
			continue
		}
		seen.Insert(condition.Type)
		out = append(out, condition)
	}
	return out
}

func setTrueCondition(conditions *[]metav1.Condition, conditionType, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func setFalseCondition(conditions *[]metav1.Condition, conditionType, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func equalDNSRecordStatus(a, b dnsv1alpha1.DNSRecordStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration || a.ZoneConfigMapName != b.ZoneConfigMapName {
		return false
	}
	if len(a.RenderedValues) != len(b.RenderedValues) || len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.RenderedValues {
		if a.RenderedValues[i] != b.RenderedValues[i] {
			return false
		}
	}
	for i := range a.Conditions {
		if !conditionEquals(a.Conditions[i], b.Conditions[i]) {
			return false
		}
	}
	return true
}

func equalPublishedServiceStatus(a, b publishv1alpha1.PublishedServiceStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration || a.Hostname != b.Hostname || a.URL != b.URL {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if !conditionEquals(a.Conditions[i], b.Conditions[i]) {
			return false
		}
	}
	return true
}

func conditionEquals(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}
