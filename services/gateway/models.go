package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Protocol string

const (
	ProtocolChat      Protocol = "chat"
	ProtocolResponses Protocol = "responses"
	ProtocolAnthropic Protocol = "anthropic"
)

func validProtocol(p Protocol) bool {
	return p == ProtocolChat || p == ProtocolResponses || p == ProtocolAnthropic
}

type Tier string

const (
	TierZen Tier = "zen"
	TierGo  Tier = "go"
)

type modelRoute struct {
	ID       string
	Tier     Tier
	Protocol Protocol
}

type modelCatalog struct {
	mu        sync.RWMutex
	zen       map[string]bool
	goModels  map[string]bool
	protocols map[string]Protocol
	updatedAt time.Time
	prefer    Tier
}

type modelCatalogSnapshot struct {
	Zen       int
	Go        int
	Total     int
	Exposed   int
	UpdatedAt time.Time
}

func newModelCatalog(prefer Tier, overrides map[string]string) *modelCatalog {
	protocols := make(map[string]Protocol, len(overrides))
	for model, protocol := range overrides {
		protocols[model] = Protocol(protocol)
	}
	return &modelCatalog{zen: map[string]bool{}, goModels: map[string]bool{}, protocols: protocols, prefer: prefer}
}

func (c *modelCatalog) Replace(zen, goModels []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if zen != nil {
		c.zen = toSet(zen)
	}
	if goModels != nil {
		c.goModels = toSet(goModels)
	}
	c.updatedAt = time.Now()
}

func (c *modelCatalog) Route(model string, hasZenKeys, hasGoKeys bool) (modelRoute, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	protocol := c.protocols[model]
	if protocol == "" {
		protocol = inferProtocol(model)
	}
	// When a model exists on both tiers, honor the configured priority.
	preferGo := c.prefer == TierGo
	if preferGo && c.goModels[model] && hasGoKeys {
		return modelRoute{ID: model, Tier: TierGo, Protocol: protocol}, nil
	}
	if c.zen[model] && hasZenKeys {
		return modelRoute{ID: model, Tier: TierZen, Protocol: protocol}, nil
	}
	if !preferGo && c.goModels[model] && hasGoKeys {
		return modelRoute{ID: model, Tier: TierGo, Protocol: protocol}, nil
	}
	// Model discovery can temporarily fail. Honor the configured priority.
	if len(c.zen) == 0 && len(c.goModels) == 0 {
		if preferGo && hasGoKeys {
			return modelRoute{ID: model, Tier: TierGo, Protocol: protocol}, nil
		}
		if hasZenKeys {
			return modelRoute{ID: model, Tier: TierZen, Protocol: protocol}, nil
		}
		if hasGoKeys {
			return modelRoute{ID: model, Tier: TierGo, Protocol: protocol}, nil
		}
	}
	return modelRoute{}, fmt.Errorf("model %q is not available in the configured Zen or Go pools", model)
}

func (c *modelCatalog) List() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]bool, len(c.zen)+len(c.goModels))
	for model := range c.zen {
		seen[model] = true
	}
	for model := range c.goModels {
		seen[model] = true
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// ListByTier 按层级返回模型列表（管理面板展示用）
func (c *modelCatalog) ListByTier() (zen, goModels []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	zen = make([]string, 0, len(c.zen))
	for m := range c.zen {
		zen = append(zen, m)
	}
	goModels = make([]string, 0, len(c.goModels))
	for m := range c.goModels {
		goModels = append(goModels, m)
	}
	sort.Strings(zen)
	sort.Strings(goModels)
	return
}

func (c *modelCatalog) Snapshot() modelCatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]bool, len(c.zen)+len(c.goModels))
	for model := range c.zen {
		seen[model] = true
	}
	for model := range c.goModels {
		seen[model] = true
	}
	exposed := 0
	for model := range seen {
		if supportedModel(model) {
			exposed++
		}
	}
	return modelCatalogSnapshot{
		Zen:       len(c.zen),
		Go:        len(c.goModels),
		Total:     len(seen),
		Exposed:   exposed,
		UpdatedAt: c.updatedAt,
	}
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

func inferProtocol(model string) Protocol {
	m := strings.ToLower(model)
	for _, prefix := range []string{"claude-", "qwen"} {
		if strings.HasPrefix(m, prefix) {
			return ProtocolAnthropic
		}
	}
	for _, prefix := range []string{"gpt-", "o1", "o3", "o4", "grok-", "muse-"} {
		if strings.HasPrefix(m, prefix) {
			return ProtocolResponses
		}
	}
	return ProtocolChat
}

func supportedModel(model string) bool {
	m := strings.ToLower(model)
	return !strings.HasPrefix(m, "gemini-")
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func fetchModels(ctx context.Context, client *http.Client, baseURL, key string) ([]string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", opencodeUserAgent())
	req.Header.Set("x-opencode-client", "cli")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, resp.StatusCode, fmt.Errorf("models endpoint returned HTTP %d", resp.StatusCode)
	}
	var payload modelsResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := dec.Decode(&payload); err != nil {
		return nil, resp.StatusCode, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	if len(models) == 0 {
		return nil, resp.StatusCode, errors.New("models endpoint returned an empty list")
	}
	return models, resp.StatusCode, nil
}
