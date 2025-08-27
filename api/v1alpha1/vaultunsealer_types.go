/*
Copyright 2025.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SecretRef is a reference to a key in a Kubernetes Secret.
type SecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
}

// VaultConnectionSpec defines how to connect to the Vault cluster.
type VaultConnectionSpec struct {
	URL                string     `json:"url"`
	CABundleSecretRef  *SecretRef `json:"caBundleSecretRef,omitempty"`
	InsecureSkipVerify bool       `json:"insecureSkipVerify,omitempty"`
}

// ModeSpec defines the unsealing strategy.
type ModeSpec struct {
	HA bool `json:"ha"`
}

// VaultUnsealerSpec defines the desired state of VaultUnsealer.
type VaultUnsealerSpec struct {
	Vault                VaultConnectionSpec `json:"vault"`
	UnsealKeysSecretRefs []SecretRef         `json:"unsealKeysSecretRefs"`
	Interval             *metav1.Duration    `json:"interval,omitempty"`
	VaultLabelSelector   string              `json:"vaultLabelSelector"`
	Mode                 ModeSpec            `json:"mode"`
	KeyThreshold         int                 `json:"keyThreshold,omitempty"`
}

// Condition represents the state of a resource.
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// VaultUnsealerStatus defines the observed state of VaultUnsealer.
type VaultUnsealerStatus struct {
	PodsChecked       []string     `json:"podsChecked,omitempty"`
	UnsealedPods      []string     `json:"unsealedPods,omitempty"`
	Conditions        []Condition  `json:"conditions,omitempty"`
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VaultUnsealer is the Schema for the vaultunsealers API.
// +kubebuilder:webhook:verbs=create;update,path=/validate-ops-autounseal-vault-io-v1alpha1-vaultunsealer,mutating=false,failurePolicy=fail,groups=ops.autounseal.vault.io,resources=vaultunsealers,versions=v1alpha1,name=vvaultunsealer.kb.io,sideEffects=None,admissionReviewVersions=v1
type VaultUnsealer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VaultUnsealerSpec   `json:"spec,omitempty"`
	Status VaultUnsealerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VaultUnsealerList contains a list of VaultUnsealer.
type VaultUnsealerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VaultUnsealer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VaultUnsealer{}, &VaultUnsealerList{})
}
