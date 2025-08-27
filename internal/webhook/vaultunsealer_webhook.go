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
	"fmt"
	"net/url"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

// log is for logging in this package.
var vaultunsealeradmissionlog = logf.Log.WithName("vaultunsealer-admission")

// VaultUnsealerValidator validates VaultUnsealer resources
type VaultUnsealerValidator struct {
	Client client.Client
}

//+kubebuilder:webhook:path=/validate-ops-autounseal-vault-io-v1alpha1-vaultunsealer,mutating=false,failurePolicy=fail,sideEffects=None,groups=ops.autounseal.vault.io,resources=vaultunsealers,verbs=create;update,versions=v1alpha1,name=vvaultunsealer.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &VaultUnsealerValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *VaultUnsealerValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	vaultUnsealer := obj.(*opsv1alpha1.VaultUnsealer)
	vaultunsealeradmissionlog.Info("validate create", "name", vaultUnsealer.Name)

	return v.validateVaultUnsealer(ctx, vaultUnsealer)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VaultUnsealerValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	vaultUnsealer := newObj.(*opsv1alpha1.VaultUnsealer)
	vaultunsealeradmissionlog.Info("validate update", "name", vaultUnsealer.Name)

	return v.validateVaultUnsealer(ctx, vaultUnsealer)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VaultUnsealerValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	vaultUnsealer := obj.(*opsv1alpha1.VaultUnsealer)
	vaultunsealeradmissionlog.Info("validate delete", "name", vaultUnsealer.Name)

	// Allow all deletions
	return nil, nil
}

// validateVaultUnsealer performs comprehensive validation of VaultUnsealer spec
func (v *VaultUnsealerValidator) validateVaultUnsealer(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) (admission.Warnings, error) {
	var allErrs field.ErrorList
	var warnings admission.Warnings

	// Validate Vault connection configuration
	if errs := v.validateVaultConnection(vaultUnsealer.Spec.Vault); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate unseal keys secret references
	if errs := v.validateUnsealKeysSecretRefs(vaultUnsealer.Spec.UnsealKeysSecretRefs); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate vault label selector
	if errs := v.validateVaultLabelSelector(vaultUnsealer.Spec.VaultLabelSelector); len(errs) > 0 {
		allErrs = append(allErrs, errs...)
	}

	// Validate key threshold (only if we have secret refs to avoid false warnings)
	if len(vaultUnsealer.Spec.UnsealKeysSecretRefs) > 0 {
		if errs, warns := v.validateKeyThreshold(vaultUnsealer.Spec.KeyThreshold, len(vaultUnsealer.Spec.UnsealKeysSecretRefs)); len(errs) > 0 || len(warns) > 0 {
			allErrs = append(allErrs, errs...)
			warnings = append(warnings, warns...)
		}
	}

	// Validate interval if specified
	if vaultUnsealer.Spec.Interval != nil {
		if errs := v.validateInterval(*vaultUnsealer.Spec.Interval); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
		}
	}

	// Validate mode configuration
	if errs, warns := v.validateMode(vaultUnsealer.Spec.Mode); len(errs) > 0 || len(warns) > 0 {
		allErrs = append(allErrs, errs...)
		warnings = append(warnings, warns...)
	}

	if len(allErrs) > 0 {
		return warnings, allErrs.ToAggregate()
	}

	return warnings, nil
}

// validateVaultConnection validates Vault connection specification
func (v *VaultUnsealerValidator) validateVaultConnection(vault opsv1alpha1.VaultConnectionSpec) field.ErrorList {
	var allErrs field.ErrorList
	fldPath := field.NewPath("spec", "vault")

	// Validate URL
	if vault.URL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("url"), "Vault URL is required"))
	} else {
		// Parse and validate URL format
		parsedURL, err := url.Parse(vault.URL)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("url"), vault.URL, fmt.Sprintf("invalid URL format: %v", err)))
		} else {
			// Validate scheme
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("url"), vault.URL, "URL scheme must be http or https"))
			}
			// Validate host
			if parsedURL.Host == "" {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("url"), vault.URL, "URL must include host"))
			}
		}
	}

	// Validate CA bundle secret reference if provided
	if vault.CABundleSecretRef != nil {
		if errs := v.validateSecretRef(*vault.CABundleSecretRef, fldPath.Child("caBundleSecretRef")); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
		}
	}

	return allErrs
}

// validateUnsealKeysSecretRefs validates unseal keys secret references
func (v *VaultUnsealerValidator) validateUnsealKeysSecretRefs(secretRefs []opsv1alpha1.SecretRef) field.ErrorList {
	var allErrs field.ErrorList
	fldPath := field.NewPath("spec", "unsealKeysSecretRefs")

	if len(secretRefs) == 0 {
		allErrs = append(allErrs, field.Required(fldPath, "at least one unseal keys secret reference is required"))
		return allErrs
	}

	// Validate each secret reference
	for i, secretRef := range secretRefs {
		if errs := v.validateSecretRef(secretRef, fldPath.Index(i)); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
		}
	}

	// Check for duplicate secret references
	seen := make(map[string]int)
	for i, secretRef := range secretRefs {
		key := fmt.Sprintf("%s/%s/%s", secretRef.Namespace, secretRef.Name, secretRef.Key)
		if prevIndex, exists := seen[key]; exists {
			allErrs = append(allErrs, field.Duplicate(fldPath.Index(i), fmt.Sprintf("duplicate secret reference (same as index %d)", prevIndex)))
		}
		seen[key] = i
	}

	return allErrs
}

// validateSecretRef validates a single secret reference
func (v *VaultUnsealerValidator) validateSecretRef(secretRef opsv1alpha1.SecretRef, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Validate name
	if secretRef.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), "secret name is required"))
	}

	// Validate key
	if secretRef.Key == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("key"), "secret key is required"))
	}

	// Namespace validation (optional, defaults to VaultUnsealer namespace)
	if secretRef.Namespace != "" {
		if len(secretRef.Namespace) > 63 {
			allErrs = append(allErrs, field.TooLong(fldPath.Child("namespace"), secretRef.Namespace, 63))
		}
		if !isValidKubernetesName(secretRef.Namespace) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("namespace"), secretRef.Namespace, "invalid Kubernetes namespace name"))
		}
	}

	// Name validation
	if len(secretRef.Name) > 253 {
		allErrs = append(allErrs, field.TooLong(fldPath.Child("name"), secretRef.Name, 253))
	}
	if !isValidKubernetesName(secretRef.Name) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), secretRef.Name, "invalid Kubernetes secret name"))
	}

	return allErrs
}

// validateVaultLabelSelector validates the vault label selector
func (v *VaultUnsealerValidator) validateVaultLabelSelector(labelSelector string) field.ErrorList {
	var allErrs field.ErrorList
	fldPath := field.NewPath("spec", "vaultLabelSelector")

	if labelSelector == "" {
		allErrs = append(allErrs, field.Required(fldPath, "vault label selector is required"))
		return allErrs
	}

	// Basic validation - should contain key=value or just key
	// This is a simplified validation; in production, you might want to use
	// k8s.io/apimachinery/pkg/labels.Parse for full validation
	if !isValidLabelSelector(labelSelector) {
		allErrs = append(allErrs, field.Invalid(fldPath, labelSelector, "invalid label selector format"))
	}

	return allErrs
}

// validateKeyThreshold validates the key threshold configuration
func (v *VaultUnsealerValidator) validateKeyThreshold(keyThreshold int, secretRefsCount int) (field.ErrorList, admission.Warnings) {
	var allErrs field.ErrorList
	var warnings admission.Warnings
	fldPath := field.NewPath("spec", "keyThreshold")

	if keyThreshold < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, keyThreshold, "keyThreshold must be non-negative"))
	}

	// Warning if threshold is 0 (unlimited)
	if keyThreshold == 0 {
		warnings = append(warnings, "keyThreshold is 0, all available keys will be used for unsealing")
	}

	// Warning if threshold is very high relative to secret count
	if keyThreshold > secretRefsCount*10 {
		warnings = append(warnings, fmt.Sprintf("keyThreshold (%d) is much higher than the number of secret references (%d)", keyThreshold, secretRefsCount))
	}

	return allErrs, warnings
}

// validateInterval validates the reconciliation interval
func (v *VaultUnsealerValidator) validateInterval(interval metav1.Duration) field.ErrorList {
	var allErrs field.ErrorList
	fldPath := field.NewPath("spec", "interval")

	duration := interval.Duration
	if duration <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, interval.String(), "interval must be positive"))
	}

	// Warn about very short intervals (less than 10 seconds)
	if duration.Seconds() < 10 {
		// Note: This would be a warning in real implementation, but field.ErrorList doesn't support warnings
		// In practice, you'd return this as a warning through the admission.Warnings return value
	}

	// Warn about very long intervals (more than 1 hour)
	if duration.Seconds() > 3600 {
		// Note: This would be a warning in real implementation
	}

	return allErrs
}

// validateMode validates the mode configuration
func (v *VaultUnsealerValidator) validateMode(mode opsv1alpha1.ModeSpec) (field.ErrorList, admission.Warnings) {
	var allErrs field.ErrorList
	var warnings admission.Warnings

	// Currently, mode only has HA field, which is a boolean
	// Future mode configurations could be validated here

	if !mode.HA {
		warnings = append(warnings, "HA mode is disabled, unsealing will stop after the first successful pod")
	}

	return allErrs, warnings
}

// Helper functions

// isValidKubernetesName validates Kubernetes resource names
func isValidKubernetesName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}

	// Basic validation: alphanumeric, hyphens, dots allowed
	// Must start and end with alphanumeric
	if !isAlphanumeric(rune(name[0])) || !isAlphanumeric(rune(name[len(name)-1])) {
		return false
	}

	for _, char := range name {
		if !isAlphanumeric(char) && char != '-' && char != '.' {
			return false
		}
	}

	return true
}

// isValidLabelSelector performs basic label selector validation
func isValidLabelSelector(selector string) bool {
	// Very basic validation - just check it's not empty and contains valid characters
	// In production, use k8s.io/apimachinery/pkg/labels.Parse
	if selector == "" {
		return false
	}

	// Must contain alphanumeric, hyphens, underscores, dots, slashes, equals
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.=/,"
	for _, char := range selector {
		if !strings.ContainsRune(validChars, char) {
			return false
		}
	}

	return true
}

// isAlphanumeric checks if a character is alphanumeric
func isAlphanumeric(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

// SetupWithManager sets up the webhook with the Manager
func (v *VaultUnsealerValidator) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&opsv1alpha1.VaultUnsealer{}).
		WithValidator(v).
		Complete()
}
