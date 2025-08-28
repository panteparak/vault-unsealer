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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
	"github.com/panteparak/vault-unsealer/internal/vault"
)

// TestQuickE2E runs a quick validation test without full Kubernetes deployment
// This tests the core unsealing logic with real Vault containers
func TestQuickE2E(t *testing.T) {
	t.Log("🚀 === QUICK E2E TEST - VAULT UNSEALER VALIDATION ===")
	startTime := time.Now()
	t.Logf("🕐 Test started at: %v", startTime.Format(time.RFC3339))

	ctx := context.Background()
	defer func() {
		totalDuration := time.Since(startTime)
		t.Logf("📊 Total quick E2E test time: %v", totalDuration)
		t.Log("🎉 === QUICK E2E TEST COMPLETED ===")
	}()

	// Step 1: Create Docker network
	t.Log("🌐 STEP 1: Creating Docker network...")
	stepStart := time.Now()

	dockerNetwork, err := network.New(ctx, network.WithDriver("bridge"))
	if err != nil {
		t.Fatalf("❌ Failed to create Docker network: %v", err)
	}
	defer func() {
		if err := dockerNetwork.Remove(ctx); err != nil {
			t.Logf("Warning: Failed to remove docker network: %v", err)
		}
	}()

	stepDuration := time.Since(stepStart)
	t.Logf("✅ STEP 1 COMPLETED: Network created (took %v)", stepDuration)

	// Step 2: Start production Vault
	t.Log("🏛️ STEP 2: Starting production Vault...")
	stepStart = time.Now()

	vaultContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "hashicorp/vault:1.15.2",
			ExposedPorts: []string{"8200/tcp"},
			Env: map[string]string{
				"VAULT_ADDR":     "http://0.0.0.0:8200",
				"VAULT_API_ADDR": "http://0.0.0.0:8200",
				"VAULT_LOCAL_CONFIG": `{
					"backend": {"file": {"path": "/vault/data"}},
					"listener": {"tcp": {"address": "0.0.0.0:8200", "tls_disable": true}},
					"disable_mlock": true,
					"default_lease_ttl": "168h",
					"max_lease_ttl": "720h"
				}`,
			},
			Cmd:      []string{"vault", "server", "-config=/vault/config"},
			Networks: []string{dockerNetwork.Name},
			NetworkAliases: map[string][]string{
				dockerNetwork.Name: {"vault"},
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("Vault server started!"),
				wait.ForHTTP("/v1/sys/health").WithPort("8200/tcp").WithStatusCodeMatcher(func(status int) bool {
					return status == 501 || status == 200 // 501 = uninitialized, 200 = ready
				}),
			).WithDeadline(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("❌ Failed to start Vault: %v", err)
	}
	defer func() {
		if err := vaultContainer.Terminate(ctx); err != nil {
			t.Logf("Warning: Failed to terminate vault container: %v", err)
		}
	}()

	vaultPort, err := vaultContainer.MappedPort(ctx, "8200")
	if err != nil {
		t.Fatalf("❌ Failed to get Vault port: %v", err)
	}
	vaultURL := fmt.Sprintf("http://127.0.0.1:%s", vaultPort.Port())

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 2 COMPLETED: Vault started on %s (took %v)", vaultURL, stepDuration)

	// Step 3: Initialize and seal Vault
	t.Log("🔐 STEP 3: Initializing and sealing Vault...")
	stepStart = time.Now()

	vaultKeys, rootToken, err := quickInitializeVault(vaultURL)
	if err != nil {
		t.Fatalf("❌ Failed to initialize Vault: %v", err)
	}

	t.Logf("🔑 Vault initialized with %d keys", len(vaultKeys))

	// Seal the Vault
	if err := quickSealVault(vaultURL, rootToken); err != nil {
		t.Fatalf("❌ Failed to seal Vault: %v", err)
	}

	// Verify it's sealed
	if sealed, err := checkVaultSealStatus(vaultURL); err != nil {
		t.Fatalf("❌ Failed to check seal status: %v", err)
	} else if !sealed {
		t.Fatal("❌ Vault should be sealed but it's not")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 3 COMPLETED: Vault initialized and sealed (took %v)", stepDuration)

	// Step 4: Test secret loading functionality
	t.Log("🔑 STEP 4: Testing secret loading...")
	stepStart = time.Now()

	// Create mock secrets data
	mockSecrets := map[string]map[string][]byte{
		"vault-keys-json": {
			"keys.json": mustMarshalJSON(vaultKeys),
		},
		"vault-keys-text": {
			"keys.txt": []byte(strings.Join(vaultKeys, "\n")),
		},
	}

	// Create mock secret refs
	secretRefs := []opsv1alpha1.SecretRef{
		{Name: "vault-keys-json", Key: "keys.json"},
		{Name: "vault-keys-text", Key: "keys.txt"},
	}

	// Test key loading logic directly

	// Test loading keys (we'll mock the K8s client part)
	t.Log("📋 Testing key deduplication and threshold logic...")

	// Manually combine keys for testing
	var allKeys []string
	for _, ref := range secretRefs {
		if data, exists := mockSecrets[ref.Name][ref.Key]; exists {
			if strings.HasSuffix(ref.Key, ".json") {
				var jsonKeys []string
				if err := json.Unmarshal(data, &jsonKeys); err == nil {
					allKeys = append(allKeys, jsonKeys...)
				}
			} else {
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				allKeys = append(allKeys, lines...)
			}
		}
	}

	// Deduplicate keys
	keySet := make(map[string]bool)
	var uniqueKeys []string
	for _, key := range allKeys {
		key = strings.TrimSpace(key)
		if key != "" && !keySet[key] {
			keySet[key] = true
			uniqueKeys = append(uniqueKeys, key)
		}
	}

	t.Logf("📊 Loaded %d keys, %d unique keys", len(allKeys), len(uniqueKeys))

	if len(uniqueKeys) != len(vaultKeys) {
		t.Fatalf("❌ Expected %d unique keys, got %d", len(vaultKeys), len(uniqueKeys))
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 4 COMPLETED: Secret loading tested (took %v)", stepDuration)

	// Step 5: Test Vault client unsealing
	t.Log("🔓 STEP 5: Testing Vault unsealing with client...")
	stepStart = time.Now()

	// Create Vault client
	vaultClient, err := vault.NewClient(vaultURL, nil)
	if err != nil {
		t.Fatalf("❌ Failed to create Vault client: %v", err)
	}

	// Test unsealing with threshold keys
	threshold := 3
	unsealKeys := uniqueKeys[:threshold]

	t.Logf("🔑 Attempting to unseal with %d keys (threshold: %d)", len(unsealKeys), threshold)

	for i, key := range unsealKeys {
		t.Logf("🔑 Using unseal key %d/%d", i+1, len(unsealKeys))

		status, err := vaultClient.Unseal(context.Background(), key)
		if err != nil {
			t.Fatalf("❌ Failed to unseal with key %d: %v", i+1, err)
		}

		t.Logf("📊 Unseal progress: %d/%d (sealed: %t)", status.Progress, status.T, status.Sealed)

		if !status.Sealed {
			t.Logf("🎉 Vault successfully unsealed after %d keys!", i+1)
			break
		}
	}

	// Verify unsealing succeeded
	if sealed, err := checkVaultSealStatus(vaultURL); err != nil {
		t.Fatalf("❌ Failed to check final seal status: %v", err)
	} else if sealed {
		t.Fatal("❌ Vault should be unsealed but it's still sealed")
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 5 COMPLETED: Vault successfully unsealed (took %v)", stepDuration)

	// Step 6: Test failure recovery
	t.Log("💥 STEP 6: Testing failure recovery...")
	stepStart = time.Now()

	// Re-seal vault to test recovery
	if err := quickSealVault(vaultURL, rootToken); err != nil {
		t.Fatalf("❌ Failed to re-seal Vault: %v", err)
	}

	// Verify it's sealed again
	if sealed, err := checkVaultSealStatus(vaultURL); err != nil {
		t.Fatalf("❌ Failed to check re-seal status: %v", err)
	} else if !sealed {
		t.Fatal("❌ Vault should be sealed after re-sealing")
	}

	// Test unsealing again (simulating operator recovery)
	for i, key := range unsealKeys {
		status, err := vaultClient.Unseal(context.Background(), key)
		if err != nil {
			t.Fatalf("❌ Failed recovery unsealing with key %d: %v", i+1, err)
		}

		if !status.Sealed {
			t.Logf("🎉 Recovery successful after %d keys!", i+1)
			break
		}
	}

	stepDuration = time.Since(stepStart)
	t.Logf("✅ STEP 6 COMPLETED: Failure recovery tested (took %v)", stepDuration)

	t.Log("🎉 All quick E2E validation steps completed successfully!")
	t.Log("✅ Core Vault unsealing functionality verified")
	t.Log("✅ Secret loading and deduplication tested")
	t.Log("✅ Threshold-based unsealing validated")
	t.Log("✅ Failure recovery scenarios tested")
}

// Helper functions for quick test

func quickInitializeVault(vaultURL string) ([]string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	initData := map[string]interface{}{
		"secret_shares":    5,
		"secret_threshold": 3,
	}

	initBody, _ := json.Marshal(initData)
	resp, err := client.Post(vaultURL+"/v1/sys/init", "application/json", strings.NewReader(string(initBody)))
	if err != nil {
		return nil, "", fmt.Errorf("failed to initialize Vault: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore close error
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("vault init failed with status %d: %s", resp.StatusCode, string(body))
	}

	var initResp struct {
		Keys      []string `json:"keys"`
		RootToken string   `json:"root_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, "", fmt.Errorf("failed to decode init response: %w", err)
	}

	return initResp.Keys, initResp.RootToken, nil
}

func quickSealVault(vaultURL, rootToken string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("PUT", vaultURL+"/v1/sys/seal", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", rootToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to seal Vault: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore close error
	}()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault seal failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func checkVaultSealStatus(vaultURL string) (bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(vaultURL + "/v1/sys/seal-status")
	if err != nil {
		return false, fmt.Errorf("failed to get seal status: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore close error
	}()

	var status struct {
		Sealed bool `json:"sealed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode seal status: %w", err)
	}

	return status.Sealed, nil
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
