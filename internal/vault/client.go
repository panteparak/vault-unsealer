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

package vault

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/hashicorp/vault/api"
)

type Client struct {
	client *api.Client
}

type SealStatus struct {
	Sealed      bool   `json:"sealed"`
	T           int    `json:"t"`
	N           int    `json:"n"`
	Progress    int    `json:"progress"`
	Nonce       string `json:"nonce"`
	Version     string `json:"version"`
	BuildDate   string `json:"build_date"`
	Migration   bool   `json:"migration"`
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type UnsealResponse struct {
	Sealed   bool `json:"sealed"`
	T        int  `json:"t"`
	N        int  `json:"n"`
	Progress int  `json:"progress"`
}

func NewClient(address string, tlsConfig *tls.Config) (*Client, error) {
	config := api.DefaultConfig()
	config.Address = address

	if tlsConfig != nil {
		if config.HttpClient.Transport == nil {
			config.HttpClient.Transport = &http.Transport{}
		}
		if transport, ok := config.HttpClient.Transport.(*http.Transport); ok {
			transport.TLSClientConfig = tlsConfig
		}
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	return &Client{client: client}, nil
}

func (c *Client) GetSealStatus(ctx context.Context) (*SealStatus, error) {
	req := c.client.NewRequest("GET", "/v1/sys/seal-status")
	resp, err := c.client.RawRequestWithContext(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get seal status: %w", err)
	}
	defer resp.Body.Close()

	var status SealStatus
	if err := resp.DecodeJSON(&status); err != nil {
		return nil, fmt.Errorf("failed to decode seal status: %w", err)
	}

	return &status, nil
}

func (c *Client) Unseal(ctx context.Context, key string) (*UnsealResponse, error) {
	req := c.client.NewRequest("POST", "/v1/sys/unseal")
	if err := req.SetJSONBody(map[string]interface{}{"key": key}); err != nil {
		return nil, fmt.Errorf("failed to set request body: %w", err)
	}

	resp, err := c.client.RawRequestWithContext(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to unseal: %w", err)
	}
	defer resp.Body.Close()

	var unsealResp UnsealResponse
	if err := resp.DecodeJSON(&unsealResp); err != nil {
		return nil, fmt.Errorf("failed to decode unseal response: %w", err)
	}

	return &unsealResp, nil
}
