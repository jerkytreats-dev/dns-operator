package tailscale

import (
	"context"
	"fmt"
	"strings"

	"github.com/jerkytreats/dns-operator/api/common"
	tailscalev1alpha1 "github.com/jerkytreats/dns-operator/api/tailscale/v1alpha1"
	"github.com/jerkytreats/dns-operator/internal/tailnetdns"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type tailnetAuthRefs struct {
	SecretRef              *common.SecretKeyReference
	OAuthClientCredentials *tailscalev1alpha1.TailnetOAuthClientCredentials
}

func dnsConfigAuthRefs(auth tailscalev1alpha1.TailnetDNSAuth) tailnetAuthRefs {
	return tailnetAuthRefs{
		SecretRef:              auth.SecretRef,
		OAuthClientCredentials: auth.OAuthClientCredentials,
	}
}

func dnsEndpointAuthRefs(auth tailscalev1alpha1.TailnetDNSEndpointAuth) tailnetAuthRefs {
	return tailnetAuthRefs{
		SecretRef:              auth.SecretRef,
		OAuthClientCredentials: auth.OAuthClientCredentials,
	}
}

func resolveTailnetAuth(ctx context.Context, kube client.Client, ownerNamespace string, refs tailnetAuthRefs) (tailnetdns.AuthConfig, error) {
	hasSecretRef := refs.SecretRef != nil
	hasOAuth := refs.OAuthClientCredentials != nil
	if hasSecretRef == hasOAuth {
		return tailnetdns.AuthConfig{}, fmt.Errorf("exactly one of auth.secretRef or auth.oauthClientCredentials must be set")
	}

	if hasSecretRef {
		token, err := readSecretRefValue(ctx, kube, ownerNamespace, *refs.SecretRef)
		if err != nil {
			return tailnetdns.AuthConfig{}, err
		}
		return tailnetdns.AuthConfig{APIToken: token}, nil
	}

	oauth := refs.OAuthClientCredentials
	clientID, err := readSecretRefValue(ctx, kube, ownerNamespace, oauth.ClientIDSecretRef)
	if err != nil {
		return tailnetdns.AuthConfig{}, fmt.Errorf("resolve oauth client id: %w", err)
	}
	clientSecret, err := readSecretRefValue(ctx, kube, ownerNamespace, oauth.ClientSecretSecretRef)
	if err != nil {
		return tailnetdns.AuthConfig{}, fmt.Errorf("resolve oauth client secret: %w", err)
	}

	scopes := append([]string(nil), oauth.Scopes...)
	if len(scopes) == 0 {
		scopes = []string{tailnetdns.DefaultOAuthScope}
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) != tailnetdns.DefaultOAuthScope {
			return tailnetdns.AuthConfig{}, fmt.Errorf("unsupported tailscale oauth scope %q", strings.TrimSpace(scope))
		}
	}
	return tailnetdns.AuthConfig{
		OAuth: &tailnetdns.OAuthClientCredentials{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
		},
	}, nil
}

func readSecretRefValue(ctx context.Context, kube client.Client, ownerNamespace string, ref common.SecretKeyReference) (string, error) {
	if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Key) == "" {
		return "", fmt.Errorf("secretRef name and key are required")
	}

	secretNamespace, err := namespaceForSecretRef(ownerNamespace, ref.Namespace)
	if err != nil {
		return "", err
	}

	var secret corev1.Secret
	if err := kube.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("get credentials secret: %w", err)
	}
	value, found := secret.Data[ref.Key]
	if !found || len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s missing key %q", secretNamespace, ref.Name, ref.Key)
	}
	return string(value), nil
}

func tailnetAuthReferencesSecret(ownerNamespace string, refs tailnetAuthRefs, secretNamespace, secretName string) bool {
	if refs.SecretRef != nil && secretRefMatches(ownerNamespace, *refs.SecretRef, secretNamespace, secretName) {
		return true
	}
	if refs.OAuthClientCredentials == nil {
		return false
	}
	return secretRefMatches(ownerNamespace, refs.OAuthClientCredentials.ClientIDSecretRef, secretNamespace, secretName) ||
		secretRefMatches(ownerNamespace, refs.OAuthClientCredentials.ClientSecretSecretRef, secretNamespace, secretName)
}

func secretRefMatches(ownerNamespace string, ref common.SecretKeyReference, secretNamespace, secretName string) bool {
	refNamespace, err := namespaceForSecretRef(ownerNamespace, ref.Namespace)
	if err != nil {
		return false
	}
	return refNamespace == secretNamespace && ref.Name == secretName
}
