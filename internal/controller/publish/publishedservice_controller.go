package publish

import (
	"context"
	"fmt"
	"time"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const publishRequeueInterval = 2 * time.Minute

type PublishedServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=publish.jerkytreats.dev,resources=publishedservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=publish.jerkytreats.dev,resources=publishedservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=certificate.jerkytreats.dev,resources=certificatebundles,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
func (r *PublishedServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	started := time.Now()
	defer func() {
		observability.ObserveReconcile("publish-publishedservice", started, result, err)
	}()

	if req.Namespace == "" {
		return ctrl.Result{}, nil
	}

	var services publishv1alpha1.PublishedServiceList
	if err = r.List(ctx, &services, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list published services: %w", err)
	}

	var bundles certificatev1alpha1.CertificateBundleList
	if err = r.List(ctx, &bundles, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list certificate bundles: %w", err)
	}

	bundleSecrets, err := r.bundleSecrets(ctx, bundles.Items)
	if err != nil {
		return ctrl.Result{}, err
	}

	rendered, serviceStatuses, err := publishdomain.BuildRuntime(services.Items, bundles.Items, bundleSecrets)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build runtime config: %w", err)
	}

	configMapOperation, runtimeErr := r.reconcileRuntimeConfigMap(ctx, req.Namespace, rendered)
	if runtimeErr == nil {
		observability.RecordArtifactUpdate("publish-publishedservice", "runtime_configmap", configMapOperation)
		secretOperation, secretErr := r.reconcileRuntimeCertificates(ctx, req.Namespace, rendered)
		if secretErr == nil {
			observability.RecordArtifactUpdate("publish-publishedservice", "runtime_certificates", secretOperation)
		}
		runtimeErr = secretErr
	}

	needsRequeue := runtimeErr != nil
	for i := range services.Items {
		service := &services.Items[i]
		key := types.NamespacedName{Name: service.Name, Namespace: service.Namespace}
		status := serviceStatuses[key]
		if err := r.patchPublishedServiceStatus(ctx, service, status, runtimeErr); err != nil {
			return ctrl.Result{}, err
		}
		if status.Err != nil {
			needsRequeue = true
		}
	}

	if runtimeErr != nil {
		return ctrl.Result{RequeueAfter: publishRequeueInterval}, nil
	}
	if needsRequeue {
		return ctrl.Result{RequeueAfter: publishRequeueInterval}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PublishedServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	anyChange := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return true },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&publishv1alpha1.PublishedService{}, builder.WithPredicates(anyChange)).
		Watches(
			&certificatev1alpha1.CertificateBundle{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return r.requestsForNamespaceServices(ctx, obj.GetNamespace())
			}),
			builder.WithPredicates(anyChange),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return r.requestsForSecret(ctx, obj)
			}),
			builder.WithPredicates(anyChange),
		).
		Named("publish-publishedservice").
		Complete(r)
}

func (r *PublishedServiceReconciler) bundleSecrets(
	ctx context.Context,
	bundles []certificatev1alpha1.CertificateBundle,
) (map[types.NamespacedName]*corev1.Secret, error) {
	secretRefs := sets.New[types.NamespacedName]()
	for _, bundle := range bundles {
		if bundle.Status.CertificateSecretRef == nil {
			continue
		}
		namespace := bundle.Status.CertificateSecretRef.Namespace
		if namespace == "" {
			namespace = bundle.Namespace
		}
		secretRefs.Insert(types.NamespacedName{Name: bundle.Status.CertificateSecretRef.Name, Namespace: namespace})
	}

	secrets := make(map[types.NamespacedName]*corev1.Secret, secretRefs.Len())
	for secretRef := range secretRefs {
		var secret corev1.Secret
		if err := r.Get(ctx, secretRef, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("get certificate secret %s/%s: %w", secretRef.Namespace, secretRef.Name, err)
		}
		secrets[secretRef] = secret.DeepCopy()
	}
	return secrets, nil
}

func (r *PublishedServiceReconciler) reconcileRuntimeConfigMap(
	ctx context.Context,
	namespace string,
	rendered publishdomain.RenderedRuntime,
) (string, error) {
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rendered.ConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dns-operator",
				"app.kubernetes.io/managed-by": "dns-operator",
			},
			Annotations: map[string]string{
				"publish.jerkytreats.dev/runtime-hash": rendered.Hash,
			},
		},
		Data: map[string]string{
			rendered.ConfigMapKey: rendered.Content,
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

	if labelsEqual(current.Labels, desired.Labels) &&
		annotationsEqual(current.Annotations, desired.Annotations) &&
		stringMapEqual(current.Data, desired.Data) {
		return observability.OperationNoop, nil
	}

	current.Labels = desired.Labels
	current.Annotations = desired.Annotations
	current.Data = desired.Data
	return observability.OperationUpdate, r.Update(ctx, current)
}

func (r *PublishedServiceReconciler) reconcileRuntimeCertificates(
	ctx context.Context,
	namespace string,
	rendered publishdomain.RenderedRuntime,
) (string, error) {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rendered.CertificatesSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dns-operator",
				"app.kubernetes.io/managed-by": "dns-operator",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: rendered.CertificateSecretData,
	}

	current := &corev1.Secret{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return observability.OperationCreate, r.Create(ctx, desired)
		}
		return "", err
	}

	if labelsEqual(current.Labels, desired.Labels) && current.Type == desired.Type && secretDataEqual(current.Data, desired.Data) {
		return observability.OperationNoop, nil
	}

	current.Labels = desired.Labels
	current.Type = desired.Type
	current.Data = desired.Data
	return observability.OperationUpdate, r.Update(ctx, current)
}

func (r *PublishedServiceReconciler) patchPublishedServiceStatus(
	ctx context.Context,
	service *publishv1alpha1.PublishedService,
	runtimeStatus publishdomain.ServiceRuntimeStatus,
	runtimeErr error,
) error {
	base := service.DeepCopy()
	service.Status.ObservedGeneration = service.Generation
	service.Status.Hostname = service.Spec.Hostname
	service.Status.URL = runtimeStatus.URL
	service.Status.RenderedConfigMapName = ""
	if runtimeStatus.RuntimeRequired {
		service.Status.RenderedConfigMapName = runtimeStatus.RenderedConfigMap
	}
	service.Status.CertificateBundleRef = runtimeStatus.CertificateBundleRef
	service.Status.Conditions = resetConditions(service.Status.Conditions)

	if runtimeStatus.RuntimeRequired && runtimeStatus.Err != nil && runtimeStatus.Reason == publishdomain.ReasonValidationFailed {
		setFalseCondition(&service.Status.Conditions, common.ConditionInputValid, "ValidationFailed", runtimeStatus.Err.Error(), service.Generation)
		setFalseCondition(&service.Status.Conditions, common.ConditionAccepted, "Rejected", runtimeStatus.Err.Error(), service.Generation)
		setFalseCondition(&service.Status.Conditions, common.ConditionReferencesResolved, "ValidationFailed", runtimeStatus.Err.Error(), service.Generation)
		setFalseCondition(&service.Status.Conditions, common.ConditionCertificateReady, "ValidationFailed", runtimeStatus.Err.Error(), service.Generation)
		setFalseCondition(&service.Status.Conditions, common.ConditionRuntimeReady, "ValidationFailed", runtimeStatus.Err.Error(), service.Generation)
		setFalseCondition(&service.Status.Conditions, common.ConditionReady, "ValidationFailed", runtimeStatus.Err.Error(), service.Generation)
	} else {
		setTrueCondition(&service.Status.Conditions, common.ConditionInputValid, "Validated", "published service accepted for runtime derivation", service.Generation)
		setTrueCondition(&service.Status.Conditions, common.ConditionAccepted, "Accepted", "published service accepted for runtime derivation", service.Generation)

		switch {
		case !runtimeStatus.RuntimeRequired:
			service.Status.CertificateBundleRef = nil
			service.Status.RenderedConfigMapName = ""
			setTrueCondition(&service.Status.Conditions, common.ConditionReferencesResolved, "NotRequired", "https runtime is not required for dnsOnly services", service.Generation)
			setTrueCondition(&service.Status.Conditions, common.ConditionCertificateReady, "NotRequired", "https runtime is not required for dnsOnly services", service.Generation)
			setTrueCondition(&service.Status.Conditions, common.ConditionRuntimeReady, "NotRequired", "https runtime is not required for dnsOnly services", service.Generation)
			setAggregatedReadyCondition(&service.Status.Conditions, service.Generation)
		case runtimeStatus.Err != nil:
			setFalseCondition(&service.Status.Conditions, common.ConditionReferencesResolved, runtimeStatus.Reason, runtimeStatus.Err.Error(), service.Generation)
			setFalseCondition(&service.Status.Conditions, common.ConditionCertificateReady, runtimeStatus.Reason, runtimeStatus.Err.Error(), service.Generation)
			setFalseCondition(&service.Status.Conditions, common.ConditionRuntimeReady, runtimeStatus.Reason, runtimeStatus.Err.Error(), service.Generation)
			setFalseCondition(&service.Status.Conditions, common.ConditionReady, runtimeStatus.Reason, runtimeStatus.Err.Error(), service.Generation)
		case runtimeErr != nil:
			setTrueCondition(&service.Status.Conditions, common.ConditionReferencesResolved, "Resolved", "certificate bundle and secret are ready for runtime rendering", service.Generation)
			setTrueCondition(&service.Status.Conditions, common.ConditionCertificateReady, "Resolved", "shared certificate coverage is ready for runtime rendering", service.Generation)
			setFalseCondition(&service.Status.Conditions, common.ConditionRuntimeReady, "RenderFailed", runtimeErr.Error(), service.Generation)
			setFalseCondition(&service.Status.Conditions, common.ConditionReady, "RenderFailed", runtimeErr.Error(), service.Generation)
		default:
			setTrueCondition(&service.Status.Conditions, common.ConditionReferencesResolved, "Resolved", "certificate bundle and secret are ready for runtime rendering", service.Generation)
			setTrueCondition(&service.Status.Conditions, common.ConditionCertificateReady, "Resolved", "shared certificate coverage is ready for runtime rendering", service.Generation)
			setTrueCondition(&service.Status.Conditions, common.ConditionRuntimeReady, "Rendered", "published service rendered into the Caddy runtime config", service.Generation)
			setAggregatedReadyCondition(&service.Status.Conditions, service.Generation)
		}
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
		common.ConditionInputValid,
		common.ConditionAccepted,
		common.ConditionReferencesResolved,
		common.ConditionCertificateReady,
		common.ConditionRuntimeReady,
		common.ConditionReady,
	)
	return nil
}

func (r *PublishedServiceReconciler) requestsForNamespaceServices(ctx context.Context, namespace string) []reconcile.Request {
	var services publishv1alpha1.PublishedServiceList
	if err := r.List(ctx, &services, client.InNamespace(namespace)); err != nil {
		return nil
	}
	if len(services.Items) == 0 {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: publishdomain.RuntimeConfigMapName, Namespace: namespace}}}
	}

	requests := make([]reconcile.Request, 0, len(services.Items))
	for _, service := range services.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: service.Name, Namespace: service.Namespace},
		})
	}
	return requests
}

func (r *PublishedServiceReconciler) requestsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	var bundles certificatev1alpha1.CertificateBundleList
	if err := r.List(ctx, &bundles, client.InNamespace(secret.GetNamespace())); err != nil {
		return nil
	}

	for _, bundle := range bundles.Items {
		if bundle.Status.CertificateSecretRef == nil {
			continue
		}
		namespace := bundle.Status.CertificateSecretRef.Namespace
		if namespace == "" {
			namespace = bundle.Namespace
		}
		if namespace == secret.GetNamespace() && bundle.Status.CertificateSecretRef.Name == secret.GetName() {
			return r.requestsForNamespaceServices(ctx, secret.GetNamespace())
		}
	}
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

func setAggregatedReadyCondition(conditions *[]metav1.Condition, generation int64) {
	dnsReady := meta.IsStatusConditionTrue(*conditions, common.ConditionDNSReady)
	runtimeReady := meta.IsStatusConditionTrue(*conditions, common.ConditionRuntimeReady)
	certificateReady := meta.IsStatusConditionTrue(*conditions, common.ConditionCertificateReady)
	inputValid := meta.IsStatusConditionTrue(*conditions, common.ConditionInputValid)
	accepted := meta.IsStatusConditionTrue(*conditions, common.ConditionAccepted)

	if dnsReady && runtimeReady && certificateReady && inputValid && accepted {
		setTrueCondition(conditions, common.ConditionReady, "Ready", "published service is ready for DNS and HTTPS runtime", generation)
		return
	}

	if !dnsReady {
		setFalseCondition(conditions, common.ConditionReady, "AwaitingDNS", "waiting for authoritative DNS projection", generation)
		return
	}

	setFalseCondition(conditions, common.ConditionReady, "RuntimePending", "waiting for HTTPS runtime readiness", generation)
}

func equalPublishedServiceStatus(a, b publishv1alpha1.PublishedServiceStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration ||
		a.Hostname != b.Hostname ||
		a.URL != b.URL ||
		a.RenderedConfigMapName != b.RenderedConfigMapName {
		return false
	}
	if !equalRefs(a.CertificateBundleRef, b.CertificateBundleRef) {
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

func equalRefs(a, b *common.ObjectReference) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Name == b.Name && a.Namespace == b.Namespace
}

func conditionEquals(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}

func labelsEqual(a, b map[string]string) bool {
	return stringMapEqual(a, b)
}

func annotationsEqual(a, b map[string]string) bool {
	return stringMapEqual(a, b)
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func secretDataEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, found := b[key]
		if !found || string(value) != string(other) {
			return false
		}
	}
	return true
}
