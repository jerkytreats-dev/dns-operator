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
	PublishModeHTTPSProxy = "httpsProxy"
	PublishModeDNSOnly    = "dnsOnly"
	TLSModeSharedSAN      = "sharedSAN"
	TLSModeDisabled       = "disabled"
	AuthModeNone          = "none"
)

type PublishBackendTransport struct {
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

type PublishBackend struct {
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// +kubebuilder:validation:Enum=http;https;tcp
	// +kubebuilder:default=http
	Protocol string `json:"protocol,omitempty"`

	Transport *PublishBackendTransport `json:"transport,omitempty"`
}

type PublishTLS struct {
	// +kubebuilder:validation:Enum=sharedSAN;disabled
	Mode string `json:"mode,omitempty"`
}

type PublishAuth struct {
	// +kubebuilder:validation:Enum=none
	// +kubebuilder:default=none
	Mode string `json:"mode,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.publishMode != 'httpsProxy' || has(self.backend)",message="backend is required when publishMode is httpsProxy"
// +kubebuilder:validation:XValidation:rule="self.publishMode != 'httpsProxy' || !has(self.backend) || size(self.backend.address) > 0",message="backend.address is required when publishMode is httpsProxy"
// +kubebuilder:validation:XValidation:rule="self.publishMode != 'httpsProxy' || has(self.tls)",message="tls is required when publishMode is httpsProxy"
type PublishedServiceSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Hostname string `json:"hostname"`

	// +kubebuilder:validation:Enum=httpsProxy;dnsOnly
	PublishMode string `json:"publishMode"`

	Backend *PublishBackend `json:"backend,omitempty"`

	TLS *PublishTLS `json:"tls,omitempty"`

	Auth *PublishAuth `json:"auth,omitempty"`
}

type PublishedServiceStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Hostname string `json:"hostname,omitempty"`

	URL string `json:"url,omitempty"`

	DNSRecordRef *common.ObjectReference `json:"dnsRecordRef,omitempty"`

	CertificateBundleRef *common.ObjectReference `json:"certificateBundleRef,omitempty"`

	RenderedConfigMapName string `json:"renderedConfigMapName,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ps
// +kubebuilder:printcolumn:name="Hostname",type=string,JSONPath=`.spec.hostname`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.publishMode`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
type PublishedService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PublishedServiceSpec   `json:"spec"`
	Status            PublishedServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PublishedServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PublishedService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PublishedService{}, &PublishedServiceList{})
}
