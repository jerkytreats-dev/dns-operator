/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"github.com/jerkytreats/dns-operator/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	CertificateBundleModeSharedSAN     = "sharedSAN"
	CertificateIssuerLetsEncrypt       = "letsencrypt"
	CertificateIssuerLetsEncryptStaged = "letsencrypt-staging"
	CertificateChallengeDNS01          = "dns01"
)

type BundleIssuer struct {
	// +kubebuilder:validation:Enum=letsencrypt;letsencrypt-staging
	Provider string `json:"provider"`

	// +kubebuilder:validation:Format=email
	Email string `json:"email"`
}

type BundleCloudflare struct {
	APITokenSecretRef common.SecretKeyReference `json:"apiTokenSecretRef"`
}

type BundleChallenge struct {
	// +kubebuilder:validation:Enum=dns01
	Type string `json:"type"`

	Cloudflare BundleCloudflare `json:"cloudflare"`
}

type BundleSecretTemplate struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type CertificateBundleSpec struct {
	// +kubebuilder:validation:Enum=sharedSAN
	Mode string `json:"mode"`

	PublishedServiceSelector *common.ServiceSelector `json:"publishedServiceSelector,omitempty"`

	AdditionalDomains []string `json:"additionalDomains,omitempty"`

	Issuer BundleIssuer `json:"issuer"`

	Challenge BundleChallenge `json:"challenge"`

	SecretTemplate BundleSecretTemplate `json:"secretTemplate"`

	RenewBefore metav1.Duration `json:"renewBefore,omitempty"`
}

type CertificateBundleStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	State string `json:"state,omitempty"`

	EffectiveDomains []string `json:"effectiveDomains,omitempty"`

	CertificateSecretRef *common.ObjectReference `json:"certificateSecretRef,omitempty"`

	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	LastIssuedAt *metav1.Time `json:"lastIssuedAt,omitempty"`

	LastFailureClass string `json:"lastFailureClass,omitempty"`

	ConsecutiveFailures int32 `json:"consecutiveFailures,omitempty"`

	NextAttemptAt *metav1.Time `json:"nextAttemptAt,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cb
type CertificateBundle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CertificateBundleSpec   `json:"spec"`
	Status            CertificateBundleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type CertificateBundleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CertificateBundle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CertificateBundle{}, &CertificateBundleList{})
}
