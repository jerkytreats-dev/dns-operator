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
	TailnetDNSBehaviorBootstrapAndRepair = "bootstrapAndRepair"
)

type TailnetDNSAuth struct {
	SecretRef common.SecretKeyReference `json:"secretRef"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.address) && size(self.address) > 0 && !has(self.endpointRef)) || (!has(self.address) && has(self.endpointRef))",message="exactly one of address or endpointRef must be set"
type TailnetNameserver struct {
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address,omitempty"`

	EndpointRef *common.ObjectReference `json:"endpointRef,omitempty"`
}

type TailnetBehavior struct {
	// +kubebuilder:validation:Enum=bootstrapAndRepair
	Mode string `json:"mode"`
}

type TailnetDNSConfigSpec struct {
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Zone string `json:"zone"`

	// +kubebuilder:validation:MinLength=1
	Tailnet string `json:"tailnet"`

	Nameserver TailnetNameserver `json:"nameserver"`

	Auth TailnetDNSAuth `json:"auth"`

	Behavior TailnetBehavior `json:"behavior"`
}

type TailnetDNSConfigStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	ConfiguredNameserver string `json:"configuredNameserver,omitempty"`

	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	DriftDetected bool `json:"driftDetected,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=tdc
// +kubebuilder:printcolumn:name="Zone",type=string,JSONPath=`.spec.zone`
// +kubebuilder:printcolumn:name="SplitDNSReady",type=string,JSONPath=`.status.conditions[?(@.type=="SplitDNSReady")].status`

type TailnetDNSConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TailnetDNSConfigSpec   `json:"spec"`
	Status            TailnetDNSConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TailnetDNSConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TailnetDNSConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TailnetDNSConfig{}, &TailnetDNSConfigList{})
}
