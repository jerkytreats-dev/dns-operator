package common

const (
	ConditionReady              = "Ready"
	ConditionInputValid         = "InputValid"
	ConditionReferencesResolved = "ReferencesResolved"
	ConditionCredentialsReady   = "CredentialsReady"
	ConditionDNSReady           = "DNSReady"
	ConditionCertificateReady   = "CertificateReady"
	ConditionRuntimeReady       = "RuntimeReady"
	ConditionSplitDNSReady      = "SplitDNSReady"
	ConditionEndpointReady      = "EndpointReady"
	ConditionAccepted           = "Accepted"
)

type ObjectReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

func (in *ObjectReference) DeepCopyInto(out *ObjectReference) {
	*out = *in
}

func (in *ObjectReference) DeepCopy() *ObjectReference {
	if in == nil {
		return nil
	}

	out := new(ObjectReference)
	in.DeepCopyInto(out)
	return out
}

type SecretKeyReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
}

func (in *SecretKeyReference) DeepCopyInto(out *SecretKeyReference) {
	*out = *in
}

func (in *SecretKeyReference) DeepCopy() *SecretKeyReference {
	if in == nil {
		return nil
	}

	out := new(SecretKeyReference)
	in.DeepCopyInto(out)
	return out
}

type ServiceSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

func (in *ServiceSelector) DeepCopyInto(out *ServiceSelector) {
	*out = *in
	if in.MatchLabels != nil {
		out.MatchLabels = make(map[string]string, len(in.MatchLabels))
		for key, value := range in.MatchLabels {
			out.MatchLabels[key] = value
		}
	}
}

func (in *ServiceSelector) DeepCopy() *ServiceSelector {
	if in == nil {
		return nil
	}

	out := new(ServiceSelector)
	in.DeepCopyInto(out)
	return out
}
