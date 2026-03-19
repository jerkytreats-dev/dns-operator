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
	TailnetDNSEndpointExposureModeVIPService = "tailscaleVIPService"
)

type TailnetDNSEndpointAuth struct {
	SecretRef common.SecretKeyReference `json:"secretRef"`
}

type TailnetDNSEndpointService struct {
	Ref common.ObjectReference `json:"ref"`
}

type TailnetDNSEndpointExposure struct {
	// +kubebuilder:validation:Enum=tailscaleVIPService
	Mode string `json:"mode"`

	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`
}

type TailnetDNSEndpointSpec struct {
	// +kubebuilder:validation:Pattern=`^internal\.example\.test$`
	Zone string `json:"zone"`

	// +kubebuilder:validation:MinLength=1
	Tailnet string `json:"tailnet"`

	Service TailnetDNSEndpointService `json:"service"`

	Auth TailnetDNSEndpointAuth `json:"auth"`

	Exposure TailnetDNSEndpointExposure `json:"exposure"`
}

type TailnetDNSEndpointStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	ResolvedServiceRef *common.ObjectReference `json:"resolvedServiceRef,omitempty"`

	ExposureServiceRef *common.ObjectReference `json:"exposureServiceRef,omitempty"`

	EndpointHostname string `json:"endpointHostname,omitempty"`

	EndpointDNSName string `json:"endpointDNSName,omitempty"`

	EndpointAddress string `json:"endpointAddress,omitempty"`

	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=tde
// +kubebuilder:printcolumn:name="Zone",type=string,JSONPath=`.spec.zone`
// +kubebuilder:printcolumn:name="EndpointAddress",type=string,JSONPath=`.status.endpointAddress`
// +kubebuilder:printcolumn:name="EndpointReady",type=string,JSONPath=`.status.conditions[?(@.type=="EndpointReady")].status`
type TailnetDNSEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TailnetDNSEndpointSpec   `json:"spec"`
	Status            TailnetDNSEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TailnetDNSEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TailnetDNSEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TailnetDNSEndpoint{}, &TailnetDNSEndpointList{})
}
