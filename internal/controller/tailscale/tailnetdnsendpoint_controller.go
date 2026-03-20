package tailscale

import (
	"context"
	"fmt"
	"time"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/observability"
	"github.com/jerkytreats/dns-operator/internal/tailnetdns"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const tailnetDNSEndpointServiceRefIndex = "spec.service.ref.name"

type TailnetDNSEndpointReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=tailscale.jerkytreats.dev,resources=tailnetdnsendpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=tailscale.jerkytreats.dev,resources=tailnetdnsendpoints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *TailnetDNSEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	started := time.Now()
	defer func() {
		observability.ObserveReconcile("tailscale-tailnetdnsendpoint", started, result, err)
	}()

	logger := log.FromContext(ctx)

	var endpoint tailscalev1alpha1.TailnetDNSEndpoint
	if err = r.Get(ctx, req.NamespacedName, &endpoint); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	status := tailscalev1alpha1.TailnetDNSEndpointStatus{ObservedGeneration: endpoint.Generation}

	inputErr := validateTailnetDNSEndpoint(&endpoint)
	if inputErr != nil {
		if err = r.updateStatus(ctx, &endpoint, status, inputErr, nil, nil, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	secretNamespace, secretErr := namespaceForSecretRef(endpoint.Namespace, endpoint.Spec.Auth.SecretRef.Namespace)
	if secretErr != nil {
		if err = r.updateStatus(ctx, &endpoint, status, nil, secretErr, nil, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if _, err = r.readSecretValue(ctx, secretNamespace, endpoint.Spec.Auth.SecretRef.Name, endpoint.Spec.Auth.SecretRef.Key); err != nil {
		if updateErr := r.updateStatus(ctx, &endpoint, status, nil, err, nil, nil); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	targetNamespace, refErr := namespaceForObjectRef(endpoint.Namespace, endpoint.Spec.Service.Ref.Namespace, "service")
	if refErr != nil {
		if err = r.updateStatus(ctx, &endpoint, status, nil, nil, refErr, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var targetService corev1.Service
	if err = r.Get(ctx, client.ObjectKey{Namespace: targetNamespace, Name: endpoint.Spec.Service.Ref.Name}, &targetService); err != nil {
		refErr = fmt.Errorf("get target service: %w", err)
		if updateErr := r.updateStatus(ctx, &endpoint, status, nil, nil, refErr, nil); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}
	status.ResolvedServiceRef = &common.ObjectReference{Name: targetService.Name, Namespace: targetService.Namespace}

	desiredService, buildErr := tailnetdns.BuildExposureService(&endpoint, &targetService)
	if buildErr != nil {
		if err = r.updateStatus(ctx, &endpoint, status, nil, nil, buildErr, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if err = controllerutil.SetControllerReference(&endpoint, desiredService, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	key := client.ObjectKeyFromObject(desiredService)
	exposureService := corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desiredService.Name, Namespace: desiredService.Namespace}}
	operation, err := controllerutil.CreateOrUpdate(ctx, r.Client, &exposureService, func() error {
		exposureService.Name = desiredService.Name
		exposureService.Namespace = desiredService.Namespace
		if exposureService.Labels == nil {
			exposureService.Labels = map[string]string{}
		}
		for label, value := range desiredService.Labels {
			exposureService.Labels[label] = value
		}
		if exposureService.Annotations == nil {
			exposureService.Annotations = map[string]string{}
		}
		for annotation, value := range desiredService.Annotations {
			exposureService.Annotations[annotation] = value
		}
		exposureService.Spec.Type = desiredService.Spec.Type
		exposureService.Spec.Selector = desiredService.Spec.Selector
		exposureService.Spec.Ports = desiredService.Spec.Ports
		exposureService.Spec.PublishNotReadyAddresses = desiredService.Spec.PublishNotReadyAddresses
		return controllerutil.SetControllerReference(&endpoint, &exposureService, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if operation != controllerutil.OperationResultNone && r.Recorder != nil {
		r.Recorder.Eventf(&endpoint, corev1.EventTypeNormal, "ExposureServiceApplied", "applied exposure service %s/%s", key.Namespace, key.Name)
	}

	if err = r.Get(ctx, key, &exposureService); err != nil {
		return ctrl.Result{}, err
	}
	status.ExposureServiceRef = &common.ObjectReference{Name: exposureService.Name, Namespace: exposureService.Namespace}
	observed := tailnetdns.ObserveExposureService(&exposureService)
	status.EndpointHostname = observed.EndpointHostname
	status.EndpointDNSName = observed.EndpointDNSName
	status.EndpointAddress = observed.EndpointAddress
	status.LastAppliedAt = endpoint.Status.LastAppliedAt
	if operation != controllerutil.OperationResultNone {
		now := metav1.Now()
		status.LastAppliedAt = &now
	}

	if !observed.Ready && r.Recorder != nil {
		r.Recorder.Eventf(&endpoint, corev1.EventTypeNormal, "EndpointPending", "waiting for VIP allocation on service %s/%s", exposureService.Namespace, exposureService.Name)
	}
	if observed.Ready && r.Recorder != nil {
		r.Recorder.Eventf(&endpoint, corev1.EventTypeNormal, "EndpointReady", "endpoint address %s is available", observed.EndpointAddress)
	}

	if err = r.updateStatus(ctx, &endpoint, status, nil, nil, nil, &observed); err != nil {
		return ctrl.Result{}, err
	}
	logger.V(1).Info("reconciled tailnet dns endpoint", "name", endpoint.Name, "namespace", endpoint.Namespace, "exposureService", exposureService.Name)
	return ctrl.Result{}, nil
}

func (r *TailnetDNSEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &tailscalev1alpha1.TailnetDNSEndpoint{}, tailnetDNSEndpointServiceRefIndex, func(object client.Object) []string {
		endpoint, ok := object.(*tailscalev1alpha1.TailnetDNSEndpoint)
		if !ok || endpoint.Spec.Service.Ref.Name == "" {
			return nil
		}
		return []string{endpoint.Spec.Service.Ref.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&tailscalev1alpha1.TailnetDNSEndpoint{}).
		Owns(&corev1.Service{}).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
			service, ok := object.(*corev1.Service)
			if !ok {
				return nil
			}
			var endpoints tailscalev1alpha1.TailnetDNSEndpointList
			if err := r.List(ctx, &endpoints, client.InNamespace(service.Namespace), client.MatchingFields{tailnetDNSEndpointServiceRefIndex: service.Name}); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0, len(endpoints.Items))
			for _, endpoint := range endpoints.Items {
				requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: endpoint.Name, Namespace: endpoint.Namespace}})
			}
			return requests
		})).
		Named("tailscale-tailnetdnsendpoint").
		Complete(r)
}

func (r *TailnetDNSEndpointReconciler) readSecretValue(ctx context.Context, namespace, name, key string) (string, error) {
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

func validateTailnetDNSEndpoint(endpoint *tailscalev1alpha1.TailnetDNSEndpoint) error {
	if endpoint.Spec.Zone == "" {
		return fmt.Errorf("zone is required")
	}
	if endpoint.Spec.Tailnet == "" {
		return fmt.Errorf("tailnet is required")
	}
	if endpoint.Spec.Service.Ref.Name == "" {
		return fmt.Errorf("service.ref.name is required")
	}
	if endpoint.Spec.Auth.SecretRef.Name == "" || endpoint.Spec.Auth.SecretRef.Key == "" {
		return fmt.Errorf("auth.secretRef name and key are required")
	}
	if endpoint.Spec.Exposure.Mode != tailscalev1alpha1.TailnetDNSEndpointExposureModeVIPService {
		return fmt.Errorf("unsupported exposure mode %q", endpoint.Spec.Exposure.Mode)
	}
	if endpoint.Spec.Exposure.Hostname == "" {
		return fmt.Errorf("exposure.hostname is required")
	}
	return nil
}

func namespaceForObjectRef(ownerNamespace, refNamespace, kind string) (string, error) {
	if refNamespace == "" || refNamespace == ownerNamespace {
		return ownerNamespace, nil
	}
	return "", fmt.Errorf("%s references must remain in namespace %q", kind, ownerNamespace)
}

func (r *TailnetDNSEndpointReconciler) updateStatus(
	ctx context.Context,
	endpoint *tailscalev1alpha1.TailnetDNSEndpoint,
	status tailscalev1alpha1.TailnetDNSEndpointStatus,
	inputErr error,
	credentialsErr error,
	referencesErr error,
	exposureStatus *tailnetdns.ExposureStatus,
) error {
	base := endpoint.DeepCopy()
	endpoint.Status = status
	endpoint.Status.Conditions = resetConditions(endpoint.Status.Conditions)

	if inputErr != nil {
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionInputValid, "InvalidSpec", inputErr.Error(), endpoint.Generation)
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionReady, "InvalidSpec", inputErr.Error(), endpoint.Generation)
		goto patch
	}
	setTrueCondition(&endpoint.Status.Conditions, common.ConditionInputValid, "ValidSpec", "endpoint spec is valid", endpoint.Generation)

	if referencesErr != nil {
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionReferencesResolved, "ReferenceLookupFailed", referencesErr.Error(), endpoint.Generation)
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionReady, "ReferenceLookupFailed", referencesErr.Error(), endpoint.Generation)
		goto patch
	}
	setTrueCondition(&endpoint.Status.Conditions, common.ConditionReferencesResolved, "Resolved", "service references resolved", endpoint.Generation)

	if credentialsErr != nil {
		reason := credentialsReason(credentialsErr)
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionCredentialsReady, reason, credentialsErr.Error(), endpoint.Generation)
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionReady, reason, credentialsErr.Error(), endpoint.Generation)
		goto patch
	}
	setTrueCondition(&endpoint.Status.Conditions, common.ConditionCredentialsReady, "SecretResolved", "tailscale credentials resolved", endpoint.Generation)

	if exposureStatus == nil || !exposureStatus.Ready {
		message := "waiting for Tailscale VIP address"
		if exposureStatus != nil && endpoint.Status.ExposureServiceRef != nil {
			message = fmt.Sprintf("waiting for Tailscale VIP address on service %s/%s", endpoint.Status.ExposureServiceRef.Namespace, endpoint.Status.ExposureServiceRef.Name)
		}
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionEndpointReady, "Pending", message, endpoint.Generation)
		setFalseCondition(&endpoint.Status.Conditions, common.ConditionReady, "Pending", message, endpoint.Generation)
		goto patch
	}

	setTrueCondition(&endpoint.Status.Conditions, common.ConditionEndpointReady, "Allocated", "tailnet endpoint is available", endpoint.Generation)
	setTrueCondition(&endpoint.Status.Conditions, common.ConditionReady, "Allocated", "tailnet endpoint is ready for split dns use", endpoint.Generation)

patch:
	if equalEndpointStatus(base.Status, endpoint.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, endpoint, client.MergeFrom(base)); err != nil {
		return err
	}
	observability.EmitConditionTransitions(
		r.Recorder,
		endpoint,
		base.Status.Conditions,
		endpoint.Status.Conditions,
		common.ConditionInputValid,
		common.ConditionReferencesResolved,
		common.ConditionCredentialsReady,
		common.ConditionEndpointReady,
		common.ConditionReady,
	)
	return nil
}

func equalEndpointStatus(a, b tailscalev1alpha1.TailnetDNSEndpointStatus) bool {
	if a.ObservedGeneration != b.ObservedGeneration ||
		a.EndpointHostname != b.EndpointHostname ||
		a.EndpointDNSName != b.EndpointDNSName ||
		a.EndpointAddress != b.EndpointAddress {
		return false
	}
	if !equalObjectRef(a.ResolvedServiceRef, b.ResolvedServiceRef) || !equalObjectRef(a.ExposureServiceRef, b.ExposureServiceRef) {
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
	for index := range a.Conditions {
		if !conditionEquals(a.Conditions[index], b.Conditions[index]) {
			return false
		}
	}
	return true
}

func equalObjectRef(a, b *common.ObjectReference) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Name == b.Name && a.Namespace == b.Namespace
}
