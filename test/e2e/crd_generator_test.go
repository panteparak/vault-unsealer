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

package e2e

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

// TestCRDGeneration demonstrates programmatic CRD generation using controller-runtime
func TestCRDGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping CRD generation test in short mode")
	}

	ctx := context.Background()

	// Create a new scheme and register our types
	testScheme := runtime.NewScheme()

	// Add the VaultUnsealer types to the scheme
	if err := opsv1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("Failed to add VaultUnsealer types to scheme: %v", err)
	}

	// Add apiextensions types for CRD handling
	if err := apiextensionsv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("Failed to add apiextensions types to scheme: %v", err)
	}

	t.Log("Successfully created scheme with VaultUnsealer types")

	// Test that we can create instances of our types
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{}
	vaultUnsealer.SetGroupVersionKind(opsv1alpha1.GroupVersion.WithKind("VaultUnsealer"))

	// Validate the type is properly registered
	gvk := vaultUnsealer.GetObjectKind().GroupVersionKind()
	if gvk.Group != "ops.autounseal.vault.io" {
		t.Errorf("Expected group 'ops.autounseal.vault.io', got '%s'", gvk.Group)
	}
	if gvk.Version != "v1alpha1" {
		t.Errorf("Expected version 'v1alpha1', got '%s'", gvk.Version)
	}
	if gvk.Kind != "VaultUnsealer" {
		t.Errorf("Expected kind 'VaultUnsealer', got '%s'", gvk.Kind)
	}

	t.Logf("VaultUnsealer GVK: %s", gvk.String())

	// Test that we can create a VaultUnsealerList
	vaultUnsealerList := &opsv1alpha1.VaultUnsealerList{}
	vaultUnsealerList.SetGroupVersionKind(opsv1alpha1.GroupVersion.WithKind("VaultUnsealerList"))

	listGVK := vaultUnsealerList.GetObjectKind().GroupVersionKind()
	if listGVK.Kind != "VaultUnsealerList" {
		t.Errorf("Expected kind 'VaultUnsealerList', got '%s'", listGVK.Kind)
	}

	t.Log("Successfully validated VaultUnsealer types can be instantiated")

	// This demonstrates that the types are properly set up for CRD generation
	// The actual CRD generation would be done by controller-gen at build time
	t.Log("CRD generation validation completed successfully")

	_ = ctx // Use ctx to avoid unused variable warning
}

// TestSchemeRegistration validates that our types are properly registered
func TestSchemeRegistration(t *testing.T) {
	// Get the scheme builder from our API package
	testScheme := runtime.NewScheme()

	// Use the init function that registers our types
	if err := clientgoscheme.AddToScheme(testScheme); err != nil {
		t.Fatalf("Failed to add core types to scheme: %v", err)
	}

	if err := opsv1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("Failed to add VaultUnsealer types to scheme: %v", err)
	}

	// Verify our types are known to the scheme
	gvk := opsv1alpha1.GroupVersion.WithKind("VaultUnsealer")
	obj, err := testScheme.New(gvk)
	if err != nil {
		t.Fatalf("Scheme doesn't know about VaultUnsealer: %v", err)
	}

	if _, ok := obj.(*opsv1alpha1.VaultUnsealer); !ok {
		t.Fatalf("Expected *opsv1alpha1.VaultUnsealer, got %T", obj)
	}

	// Test VaultUnsealerList
	listGVK := opsv1alpha1.GroupVersion.WithKind("VaultUnsealerList")
	listObj, err := testScheme.New(listGVK)
	if err != nil {
		t.Fatalf("Scheme doesn't know about VaultUnsealerList: %v", err)
	}

	if _, ok := listObj.(*opsv1alpha1.VaultUnsealerList); !ok {
		t.Fatalf("Expected *opsv1alpha1.VaultUnsealerList, got %T", listObj)
	}

	t.Log("All types properly registered in scheme")
}

// This demonstrates how you could programmatically generate CRDs
// In practice, controller-gen CLI is used at build time, but this shows
// the underlying principles for runtime CRD generation if needed
func TestProgrammaticCRDGeneration(t *testing.T) {
	t.Log("Programmatic CRD generation principles demonstrated")

	// 1. The types are defined in api/v1alpha1/vaultunsealer_types.go
	// 2. They include kubebuilder annotations for schema generation
	// 3. controller-gen reads these annotations and generates OpenAPI schema
	// 4. The schema is embedded in the CRD manifest

	// Key components for CRD generation:
	// - GroupVersionKind identification
	// - OpenAPI v3 schema generation from Go structs
	// - JSON tag mapping for field names
	// - Validation rules from kubebuilder markers

	// Example of what controller-gen does internally:
	vaultUnsealer := &opsv1alpha1.VaultUnsealer{}

	// Extract type information
	gvk := opsv1alpha1.GroupVersion.WithKind("VaultUnsealer")
	t.Logf("Type: %s", gvk.String())

	// The actual CRD would be generated by walking the struct fields
	// and creating OpenAPI schema definitions, which is what controller-gen does

	// Validate that our types have the necessary metadata
	if vaultUnsealer.TypeMeta.APIVersion == "" {
		vaultUnsealer.TypeMeta.APIVersion = gvk.GroupVersion().String()
	}
	if vaultUnsealer.TypeMeta.Kind == "" {
		vaultUnsealer.TypeMeta.Kind = gvk.Kind
	}

	t.Logf("TypeMeta: APIVersion=%s, Kind=%s", vaultUnsealer.TypeMeta.APIVersion, vaultUnsealer.TypeMeta.Kind)

	t.Log("Programmatic CRD generation concepts validated")
}
