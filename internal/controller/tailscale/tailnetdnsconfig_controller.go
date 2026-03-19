package tailscale

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/observability"
	"github.com/jerkytreats/dns-operator/internal/tailnetdns"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const driftCheckInterval = 10 * time.Minute
const tailnetDNSConfigEndpointRefIndex = "spec.nameserver.endpointRef.name"

type SplitDNSClientFactory func(tailnet, apiToken string) tailnetdns.SplitDNSClient

type TailnetDNSConfigReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	ClientFactory SplitDNSClientFactory
}

// +kubebuilder:rbac:groups=tailscale.jerkytreats.dev,resources=tailnetdnsconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=tailscale.jerkytreats.dev,resources=tailnetdnsconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *TailnetDNSConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	started := time.Now()
	defer func() {
		observability.ObserveReconcile("tailscale-tailnetdnsconfig", started, result, err)
	}()

	logger := log.FromContext(ctx)

	var config tailscalev1alpha1.TailnetDNSConfig
	if err = r.Get(ctx, req.NamespacedName, &config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	secretNamespace, secretNamespaceErr := namespaceForSecretRef(config.Namespace, config.Spec.Auth.SecretRef.Namespace)
	if secretNamespaceErr != nil {
		if err = r.updateStatus(ctx, &config, tailscalev1alpha1.TailnetDNSConfigStatus{
			ObservedGeneration: config.Generation,
			DriftDetected:      true,
		}, secretNamespaceErr, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: driftCheckInterval}, nil
	}

	apiToken, credErr := r.readSecretValue(ctx, secretNamespace, config.Spec.Auth.SecretRef.Name, config.Spec.Auth.SecretRef.Key)
	result = ctrl.Result{RequeueAfter: driftCheckInterval}
	if credErr != nil {
		if err = r.updateStatus(ctx, &config, tailscalev1alpha1.TailnetDNSConfigStatus{
			ObservedGeneration: config.Generation,
			DriftDetected:      true,
		}, credErr, nil); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}

	factory := r.ClientFactory
	if factory == nil {
		factory = func(tailnet, token string) tailnetdns.SplitDNSClient {
			return tailnetdns.NewHTTPClient(tailnet, token)
		}
	}

	nameserverAddress, resolveErr := r.resolveNameserverAddress(ctx, &config)
	if resolveErr != nil {
		if err = r.updateStatus(ctx, &config, tailscalev1alpha1.TailnetDNSConfigStatus{
			ObservedGeneration: config.Generation,
			DriftDetected:      true,
		}, nil, resolveErr); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}

	splitDNSResult, ensureErr := tailnetdns.EnsureSplitDNS(
		ctx,
		factory(config.Spec.Tailnet, apiToken),
		config.Spec.Zone,
		nameserverAddress,
	)
	if ensureErr != nil {
		logger.Error(ensureErr, "unable to ensure split dns")
	}

	status := tailscalev1alpha1.TailnetDNSConfigStatus{
		ObservedGeneration:   config.Generation,
		ConfiguredNameserver: splitDNSResult.ConfiguredNameserver,
		DriftDetected:        splitDNSResult.DriftDetected,
	}
	if ensureErr != nil {
		status.DriftDetected = true
	}
	if splitDNSResult.Applied {
		now := metav1.Now()
		status.LastAppliedAt = &now
	} else {
		status.LastAppliedAt = config.Status.LastAppliedAt
	}

	if err = r.updateStatus(ctx, &config, status, nil, ensureErr); err != nil {
		return ctrl.Result{}, err
	}

	return result, nil
}

func (r *TailnetDNSConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &tailscalev1alpha1.TailnetDNSConfig{}, tailnetDNSConfigEndpointRefIndex, func(object client.Object) []string {
		config, ok := object.(*tailscalev1alpha1.TailnetDNSConfig)
		if !ok || config.Spec.Nameserver.EndpointRef == nil || config.Spec.Nameserver.EndpointRef.Name == "" {
			return nil
		}
		return []string{config.Spec.Nameserver.EndpointRef.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&tailscalev1alpha1.TailnetDNSConfig{}).
		Watches(&tailscalev1alpha1.TailnetDNSEndpoint{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
			endpoint, ok := object.(*tailscalev1alpha1.TailnetDNSEndpoint)
			if !ok {
				return nil
			}
			var configs tailscalev1alpha1.TailnetDNSConfigList
			if err := r.List(ctx, &configs, client.InNamespace(endpoint.Namespace), client.MatchingFields{tailnetDNSConfigEndpointRefIndex: endpoint.Name}); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0, len(configs.Items))
			for _, config := range configs.Items {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&config)})
			}
			return requests
		})).
		Named("tailscale-tailnetdnsconfig").
		Complete(r)
}

func (r *TailnetDNSConfigReconciler) resolveNameserverAddress(ctx context.Context, config *tailscalev1alpha1.TailnetDNSConfig) (string, error) {
	if config.Spec.Nameserver.Address != "" {
		return config.Spec.Nameserver.Address, nil
	}
	if config.Spec.Nameserver.EndpointRef == nil {
		return "", fmt.Errorf("nameserver must define either address or endpointRef")
	}

	endpointNamespace, err := namespaceForObjectRef(config.Namespace, config.Spec.Nameserver.EndpointRef.Namespace, "endpoint")
	if err != nil {
		return "", err
	}

	var endpoint tailscalev1alpha1.TailnetDNSEndpoint
	if err := r.Get(ctx, client.ObjectKey{Namespace: endpointNamespace, Name: config.Spec.Nameserver.EndpointRef.Name}, &endpoint); err != nil {
		return "", fmt.Errorf("get referenced tailnet dns endpoint: %w", err)
	}

	return tailnetdns.ResolveNameserverAddress(config, &endpoint)
}

func (r *TailnetDNSConfigReconciler) readSecretValue(ctx context.Context, namespace, name, key string) (string, error) {
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

func (r *TailnetDNSConfigReconciler) updateStatus(
	ctx context.Context,
	config *tailscalev1alpha1.TailnetDNSConfig,
	status tailscalev1alpha1.TailnetDNSConfigStatus,
	credentialsErr error,
	ensureErr error,
) error {
	base := config.DeepCopy()
	config.Status = status
	config.Status.Conditions = resetConditions(config.Status.Conditions)

	if credentialsErr != nil {
		reason := credentialsReason(credentialsErr)
		setFalseCondition(&config.Status.Conditions, common.ConditionCredentialsReady, reason, credentialsErr.Error(), config.Generation)
		setFalseCondition(&config.Status.Conditions, common.ConditionSplitDNSReady, "CredentialsUnavailable", credentialsErr.Error(), config.Generation)
		setFalseCondition(&config.Status.Conditions, common.ConditionReady, "CredentialsUnavailable", credentialsErr.Error(), config.Generation)
	} else if ensureErr != nil {
		setTrueCondition(&config.Status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "tailscale credentials resolved", config.Generation)
		setFalseCondition(&config.Status.Conditions, common.ConditionSplitDNSReady, "ApplyFailed", ensureErr.Error(), config.Generation)
		setFalseCondition(&config.Status.Conditions, common.ConditionReady, "ApplyFailed", ensureErr.Error(), config.Generation)
	} else {
		setTrueCondition(&config.Status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "tailscale credentials resolved", config.Generation)
		setTrueCondition(&config.Status.Conditions, common.ConditionSplitDNSReady, "Configured", "restricted nameserver state matches desired configuration", config.Generation)
		setTrueCondition(&config.Status.Conditions, common.ConditionReady, "Configured", "split dns bootstrap and repair is healthy", config.Generation)
	}

	if equalStatus(base.Status, config.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, config, client.MergeFrom(base)); err != nil {
		return err
	}
	observability.EmitConditionTransitions(
		r.Recorder,
		config,
		base.Status.Conditions,
		config.Status.Conditions,
		common.ConditionCredentialsReady,
		common.ConditionSplitDNSReady,
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

func equalStatus(a, b tailscalev1alpha1.TailnetDNSConfigStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration ||
		a.ConfiguredNameserver != b.ConfiguredNameserver ||
		a.DriftDetected != b.DriftDetected {
		return false
	}
	if (a.LastAppliedAt == nil) != (b.LastAppliedAt == nil) {
		return false
	}
	if a.LastAppliedAt != nil && b.LastAppliedAt != nil && !a.LastAppliedAt.Equal(b.LastAppliedAt) {
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

func credentialsReason(err error) string {
	if strings.Contains(err.Error(), "must remain in namespace") {
		return "CrossNamespaceSecretRefRejected"
	}
	return "SecretUnavailable"
}
