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

package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

func TestVaultUnsealerValidator_ValidateCreate(t *testing.T) {
	tests := []struct {
		name          string
		vaultUnsealer *opsv1alpha1.VaultUnsealer
		wantErr       bool
		wantWarnings  int
		errorContains string
	}{
		{
			name: "valid VaultUnsealer",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3,
				},
			},
			wantErr:      false,
			wantWarnings: 0,
		},
		{
			name: "missing Vault URL",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "", // Missing URL
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "Vault URL is required",
		},
		{
			name: "invalid Vault URL",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "not-a-valid-url",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "URL scheme must be http or https",
		},
		{
			name: "empty unseal keys secret refs",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{}, // Empty
					VaultLabelSelector:   "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "at least one unseal keys secret reference is required",
		},
		{
			name: "missing secret name",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "", // Missing name
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "secret name is required",
		},
		{
			name: "missing secret key",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "", // Missing key
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "secret key is required",
		},
		{
			name: "duplicate secret references",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
						{
							Name: "vault-keys-1",
							Key:  "keys.json", // Duplicate
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "duplicate secret reference",
		},
		{
			name: "missing vault label selector",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "", // Missing
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3, // Avoid warning
				},
			},
			wantErr:       true,
			errorContains: "vault label selector is required",
		},
		{
			name: "negative key threshold",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: -1, // Negative
				},
			},
			wantErr:       true,
			errorContains: "keyThreshold must be non-negative",
		},
		{
			name: "zero key threshold with warning",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 0, // Zero - should warn
				},
			},
			wantErr:      false,
			wantWarnings: 1,
		},
		{
			name: "HA disabled warning",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: false, // Should warn
					},
					KeyThreshold: 0, // This also generates a warning
				},
			},
			wantErr:      false,
			wantWarnings: 2, // HA disabled + zero key threshold
		},
		{
			name: "invalid interval",
			vaultUnsealer: &opsv1alpha1.VaultUnsealer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unsealer",
					Namespace: "default",
				},
				Spec: opsv1alpha1.VaultUnsealerSpec{
					Vault: opsv1alpha1.VaultConnectionSpec{
						URL: "https://vault.example.com:8200",
					},
					UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
						{
							Name: "vault-keys-1",
							Key:  "keys.json",
						},
					},
					VaultLabelSelector: "app.kubernetes.io/name=vault",
					Mode: opsv1alpha1.ModeSpec{
						HA: true,
					},
					KeyThreshold: 3,                                            // Avoid warning
					Interval:     &metav1.Duration{Duration: -1 * time.Second}, // Negative
				},
			},
			wantErr:       true,
			errorContains: "interval must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create scheme and add our types
			scheme := runtime.NewScheme()
			err := opsv1alpha1.AddToScheme(scheme)
			require.NoError(t, err)

			// Create fake client
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			validator := &VaultUnsealerValidator{
				Client: client,
			}

			warnings, err := validator.ValidateCreate(context.TODO(), tt.vaultUnsealer)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantWarnings, len(warnings))
		})
	}
}

func TestVaultUnsealerValidator_ValidateUpdate(t *testing.T) {
	// Create scheme and add our types
	scheme := runtime.NewScheme()
	err := opsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create fake client
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := &VaultUnsealerValidator{
		Client: client,
	}

	oldVaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-unsealer",
			Namespace: "default",
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				URL: "https://vault.example.com:8200",
			},
			UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
				{
					Name: "vault-keys-1",
					Key:  "keys.json",
				},
			},
			VaultLabelSelector: "app.kubernetes.io/name=vault",
			Mode: opsv1alpha1.ModeSpec{
				HA: true,
			},
		},
	}

	newVaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-unsealer",
			Namespace: "default",
		},
		Spec: opsv1alpha1.VaultUnsealerSpec{
			Vault: opsv1alpha1.VaultConnectionSpec{
				URL: "", // Invalid URL in update
			},
			UnsealKeysSecretRefs: []opsv1alpha1.SecretRef{
				{
					Name: "vault-keys-1",
					Key:  "keys.json",
				},
			},
			VaultLabelSelector: "app.kubernetes.io/name=vault",
			Mode: opsv1alpha1.ModeSpec{
				HA: true,
			},
		},
	}

	_, err = validator.ValidateUpdate(context.TODO(), oldVaultUnsealer, newVaultUnsealer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Vault URL is required")
}

func TestVaultUnsealerValidator_ValidateDelete(t *testing.T) {
	// Create scheme and add our types
	scheme := runtime.NewScheme()
	err := opsv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	// Create fake client
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	validator := &VaultUnsealerValidator{
		Client: client,
	}

	vaultUnsealer := &opsv1alpha1.VaultUnsealer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-unsealer",
			Namespace: "default",
		},
	}

	warnings, err := validator.ValidateDelete(context.TODO(), vaultUnsealer)
	assert.NoError(t, err)
	assert.Empty(t, warnings)
}

func Test_isValidKubernetesName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid name", "my-secret-123", true},
		{"valid with dots", "my.secret.123", true},
		{"empty name", "", false},
		{"too long", string(make([]byte, 254)), false},
		{"starts with hyphen", "-invalid", false},
		{"ends with hyphen", "invalid-", false},
		{"starts with dot", ".invalid", false},
		{"ends with dot", "invalid.", false},
		{"contains invalid chars", "my_secret@123", false},
		{"valid single char", "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidKubernetesName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_isValidLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"simple key", "app", true},
		{"key-value", "app=vault", true},
		{"multiple labels", "app=vault,version=1.0", true},
		{"with namespace", "kubernetes.io/name=vault", true},
		{"empty", "", false},
		{"with spaces", "app = vault", false}, // Our simple validator doesn't handle spaces
		{"valid complex", "app.kubernetes.io/name=vault,environment=prod", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidLabelSelector(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
