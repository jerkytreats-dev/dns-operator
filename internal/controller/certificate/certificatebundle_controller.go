package certificate

import (
	"context"
	"fmt"
	"time"

	certificatev1alpha1 "github.com/jerkytreats/dns-operator/api/certificate/v1alpha1"
	"github.com/jerkytreats/dns-operator/api/common"
	publishv1alpha1 "github.com/jerkytreats/dns-operator/api/publish/v1alpha1"
	certdomain "github.com/jerkytreats/dns-operator/internal/certificate"
	"github.com/jerkytreats/dns-operator/internal/observability"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const certificateRequeueInterval = 12 * time.Hour

type CertificateBundleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=certificate.jerkytreats.dev,resources=certificatebundles,verbs=get;list;watch
// +kubebuilder:rbac:groups=certificate.jerkytreats.dev,resources=certificatebundles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=publish.jerkytreats.dev,resources=publishedservices,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *CertificateBundleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	started := time.Now()
	defer func() {
		observability.ObserveReconcile("certificate-certificatebundle", started, result, err)
	}()

	var bundle certificatev1alpha1.CertificateBundle
	if err = r.Get(ctx, req.NamespacedName, &bundle); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var services publishv1alpha1.PublishedServiceList
	if err = r.List(ctx, &services, client.InNamespace(bundle.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list published services: %w", err)
	}

	status := certificatev1alpha1.CertificateBundleStatus{
		ObservedGeneration: bundle.Generation,
		State:              certdomain.BundleStatePending,
		Conditions:         resetConditions(bundle.Status.Conditions),
	}

	secretNamespace, secretNamespaceErr := namespaceForSecretRef(bundle.Namespace, bundle.Spec.Challenge.Cloudflare.APITokenSecretRef.Namespace)
	if secretNamespaceErr != nil {
		setFalseCondition(&status.Conditions, common.ConditionCredentialsReady, "CrossNamespaceSecretRefRejected", secretNamespaceErr.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "CredentialsUnavailable", secretNamespaceErr.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "CredentialsUnavailable", secretNamespaceErr.Error(), bundle.Generation)
		status.EffectiveDomains = nil
		if err = r.patchStatus(ctx, &bundle, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	if _, err = r.readSecretValue(ctx, secretNamespace, bundle.Spec.Challenge.Cloudflare.APITokenSecretRef.Name, bundle.Spec.Challenge.Cloudflare.APITokenSecretRef.Key); err != nil {
		setFalseCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretUnavailable", err.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "CredentialsUnavailable", err.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "CredentialsUnavailable", err.Error(), bundle.Generation)
		status.EffectiveDomains = nil
		if err = r.patchStatus(ctx, &bundle, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	domains, err := certdomain.EffectiveDomains(bundle, services.Items)
	if err != nil {
		setTrueCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "challenge credentials resolved", bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "DomainDerivationFailed", err.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "DomainDerivationFailed", err.Error(), bundle.Generation)
		if err = r.patchStatus(ctx, &bundle, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	status.EffectiveDomains = domains
	if len(domains) == 0 {
		setTrueCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "challenge credentials resolved", bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "NoDomainsSelected", "no published HTTPS hosts or explicit domains selected for this bundle", bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "NoDomainsSelected", "no published HTTPS hosts or explicit domains selected for this bundle", bundle.Generation)
		if err = r.patchStatus(ctx, &bundle, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: certificateRequeueInterval}, nil
	}

	secret, expiresAt, err := certdomain.BuildTLSSecret(bundle.Spec.SecretTemplate.Name, bundle.Namespace, domains, 0)
	if err != nil {
		setTrueCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "challenge credentials resolved", bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "IssueFailed", err.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "IssueFailed", err.Error(), bundle.Generation)
		if patchErr := r.patchStatus(ctx, &bundle, status); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}
	operation, err := r.reconcileTLSSecret(ctx, secret)
	if err != nil {
		setTrueCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "challenge credentials resolved", bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionCertificateReady, "SecretPublishFailed", err.Error(), bundle.Generation)
		setFalseCondition(&status.Conditions, common.ConditionReady, "SecretPublishFailed", err.Error(), bundle.Generation)
		if patchErr := r.patchStatus(ctx, &bundle, status); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}
	observability.RecordArtifactUpdate("certificate-certificatebundle", "certificate_secret", operation)

	status.State = certdomain.BundleStateReady
	status.CertificateSecretRef = &common.ObjectReference{Name: secret.Name, Namespace: secret.Namespace}
	status.ExpiresAt = timePtrToMeta(expiresAt)
	now := metav1.Now()
	status.LastIssuedAt = &now
	setTrueCondition(&status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "challenge credentials resolved", bundle.Generation)
	setTrueCondition(&status.Conditions, common.ConditionCertificateReady, "Issued", "certificate bundle secret published", bundle.Generation)
	setTrueCondition(&status.Conditions, common.ConditionReady, "Issued", "certificate bundle is ready for runtime attachment", bundle.Generation)

	if err = r.patchStatus(ctx, &bundle, status); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: certificateRequeueInterval}, nil
}

func (r *CertificateBundleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	anyServiceChange := predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return true },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&certificatev1alpha1.CertificateBundle{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&publishv1alpha1.PublishedService{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var bundles certificatev1alpha1.CertificateBundleList
				if err := r.List(ctx, &bundles, client.InNamespace(obj.GetNamespace())); err != nil {
					return nil
				}
				requests := make([]reconcile.Request, 0, len(bundles.Items))
				for _, bundle := range bundles.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{Name: bundle.Name, Namespace: bundle.Namespace},
					})
				}
				return requests
			}),
			builder.WithPredicates(anyServiceChange),
		).
		Named("certificate-certificatebundle").
		Complete(r)
}

func (r *CertificateBundleReconciler) readSecretValue(ctx context.Context, namespace, name, key string) (string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret); err != nil {
		return "", fmt.Errorf("get credentials secret: %w", err)
	}
	value, found := secret.Data[key]
	if !found || len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s missing key %q", namespace, name, key)
	}
	return string(value), nil
}

func namespaceForSecretRef(ownerNamespace, refNamespace string) (string, error) {
	if refNamespace == "" || refNamespace == ownerNamespace {
		return ownerNamespace, nil
	}
	return "", fmt.Errorf("secret references must remain in namespace %q", ownerNamespace)
}

func (r *CertificateBundleReconciler) reconcileTLSSecret(ctx context.Context, desired *corev1.Secret) (string, error) {
	current := &corev1.Secret{}
	key := client.ObjectKeyFromObject(desired)
	if err := r.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return observability.OperationCreate, r.Create(ctx, desired)
		}
		return "", err
	}

	if current.Type == desired.Type && secretDataEqual(current.Data, desired.Data) {
		return observability.OperationNoop, nil
	}

	current.Type = desired.Type
	current.Data = desired.Data
	return observability.OperationUpdate, r.Update(ctx, current)
}

func (r *CertificateBundleReconciler) patchStatus(ctx context.Context, bundle *certificatev1alpha1.CertificateBundle, status certificatev1alpha1.CertificateBundleStatus) error {
	base := bundle.DeepCopy()
	bundle.Status = status
	if equalBundleStatus(base.Status, bundle.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, bundle, client.MergeFrom(base)); err != nil {
		return err
	}
	observability.EmitConditionTransitions(
		r.Recorder,
		bundle,
		base.Status.Conditions,
		bundle.Status.Conditions,
		common.ConditionCredentialsReady,
		common.ConditionCertificateReady,
		common.ConditionReady,
	)
	return nil
}

func resetConditions(conditions []metav1.Condition) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(conditions))
	seen := map[string]struct{}{}
	for _, condition := range conditions {
		if _, found := seen[condition.Type]; found {
			continue
		}
		seen[condition.Type] = struct{}{}
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

func equalBundleStatus(a, b certificatev1alpha1.CertificateBundleStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration || a.State != b.State {
		return false
	}
	if !equalRefs(a.CertificateSecretRef, b.CertificateSecretRef) {
		return false
	}
	if !equalTime(a.ExpiresAt, b.ExpiresAt) || !equalTime(a.LastIssuedAt, b.LastIssuedAt) {
		return false
	}
	if len(a.EffectiveDomains) != len(b.EffectiveDomains) || len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.EffectiveDomains {
		if a.EffectiveDomains[i] != b.EffectiveDomains[i] {
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

func equalRefs(a, b *common.ObjectReference) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Name == b.Name && a.Namespace == b.Namespace
}

func equalTime(a, b *metav1.Time) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Equal(b)
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

func conditionEquals(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}

func timePtrToMeta(t *time.Time) *metav1.Time {
	if t == nil {
		return nil
	}
	value := metav1.NewTime(*t)
	return &value
}
