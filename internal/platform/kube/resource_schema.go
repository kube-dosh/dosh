package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"dosh/internal/domain"
)

const resourceSchemaTTL = 10 * time.Minute
const maxSchemaDefinitions = 128

type resourceSchemaDocument struct {
	Definitions map[string]map[string]any `json:"definitions"`
}

// GetResourceSchema resolves the OpenAPI v2 definition for one discovered GVR.
// OpenAPI discovery is server-level metadata, so it is read with application
// credentials and cached independently from the calling user. The returned
// schema is advisory: Kubernetes API Server dry-run remains authoritative.
func (c *Client) GetResourceSchema(ctx context.Context, descriptor domain.ResourceDescriptor) (domain.ResourceSchema, error) {
	ctx, cancel := c.withTimeout(domain.WithoutActor(ctx))
	defer cancel()
	document, warnings, err := c.resourceSchemaDocument(ctx)
	if err != nil {
		return domain.ResourceSchema{}, err
	}
	key, schema := matchResourceSchema(document.Definitions, descriptor)
	result := domain.ResourceSchema{
		Group: descriptor.Group, Version: descriptor.Version, Resource: descriptor.Resource,
		Kind: descriptor.Kind, Source: "openapi-v2", Schema: cloneSchemaMap(schema),
		Warnings: append([]string(nil), warnings...),
	}
	if key == "" || len(schema) == 0 {
		result.Source = "unavailable"
		result.Schema = map[string]any{}
		result.Warnings = append(result.Warnings, "当前集群 OpenAPI 文档未提供该资源类型的 Schema，已回退到 YAML 编辑")
		return result, nil
	}
	references, truncated := referencedSchemaDefinitions(schema, document.Definitions)
	if truncated {
		result.Warnings = append(result.Warnings, "Schema 引用过多，部分嵌套字段说明已省略")
	}
	if len(references) > 0 {
		result.Definitions = references
	}
	return result, nil
}

func (c *Client) resourceSchemaDocument(ctx context.Context) (*resourceSchemaDocument, []string, error) {
	c.resourceSchemaMu.Lock()
	defer c.resourceSchemaMu.Unlock()
	if c.resourceSchema != nil && time.Since(c.resourceSchemaFetched) < resourceSchemaTTL {
		return c.resourceSchema, nil, nil
	}
	if c.discovery == nil || c.discovery.RESTClient() == nil {
		return nil, nil, fmt.Errorf("%w: discovery REST client is unavailable", domain.ErrUnavailable)
	}
	body, err := c.discovery.RESTClient().Get().AbsPath("/openapi/v2").SetHeader("Accept", "application/json").DoRaw(ctx)
	if err != nil {
		if c.resourceSchema != nil {
			return c.resourceSchema, []string{"OpenAPI Schema 刷新失败，当前使用最近一次成功结果"}, nil
		}
		return nil, nil, fmt.Errorf("%w: read OpenAPI schema: %v", domain.ErrUnavailable, err)
	}
	var document resourceSchemaDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, nil, fmt.Errorf("%w: decode OpenAPI schema: %v", domain.ErrUnavailable, err)
	}
	if document.Definitions == nil {
		return nil, nil, fmt.Errorf("%w: OpenAPI schema has no definitions", domain.ErrUnavailable)
	}
	c.resourceSchema = &document
	c.resourceSchemaFetched = time.Now()
	return &document, nil, nil
}

func matchResourceSchema(definitions map[string]map[string]any, descriptor domain.ResourceDescriptor) (string, map[string]any) {
	if len(definitions) == 0 || descriptor.Version == "" || descriptor.Kind == "" {
		return "", nil
	}
	expected := strings.ToLower(descriptor.Version + "." + descriptor.Kind)
	groupToken := strings.ToLower(strings.Split(descriptor.Group, ".")[0])
	if descriptor.Group == "" {
		groupToken = "core"
	}
	type candidate struct {
		key   string
		score int
	}
	var candidates []candidate
	for key := range definitions {
		lower := strings.ToLower(key)
		if !strings.HasSuffix(lower, "."+expected) {
			continue
		}
		score := 10
		if descriptor.Group != "" && strings.HasPrefix(lower, strings.ToLower(descriptor.Group)+".") {
			score = 100
		} else if strings.Contains(lower, "."+groupToken+".") {
			score = 80
		}
		candidates = append(candidates, candidate{key: key, score: score})
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].key < candidates[j].key
	})
	return candidates[0].key, definitions[candidates[0].key]
}

func referencedSchemaDefinitions(root map[string]any, definitions map[string]map[string]any) (map[string]map[string]any, bool) {
	result := make(map[string]map[string]any)
	pending := []string{"#/definitions/__root__"}
	visited := make(map[string]struct{})
	for len(pending) > 0 {
		ref := pending[0]
		pending = pending[1:]
		if _, ok := visited[ref]; ok {
			continue
		}
		visited[ref] = struct{}{}
		var schema any = root
		key := ""
		if ref != "#/definitions/__root__" {
			key = strings.TrimPrefix(ref, "#/definitions/")
			schema = definitions[key]
			if schema == nil {
				continue
			}
			if len(result) >= maxSchemaDefinitions {
				return result, true
			}
			result[key] = cloneSchemaMap(definitions[key])
		}
		for _, nested := range schemaReferences(schema) {
			if nested != ref {
				pending = append(pending, nested)
			}
		}
	}
	return result, false
}

func schemaReferences(value any) []string {
	var refs []string
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, "#/definitions/") {
			refs = append(refs, ref)
		}
		for _, nested := range typed {
			refs = append(refs, schemaReferences(nested)...)
		}
	case []any:
		for _, nested := range typed {
			refs = append(refs, schemaReferences(nested)...)
		}
	}
	return refs
}

func cloneSchemaMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return cloneSchemaValue(value).(map[string]any)
}

func cloneSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = cloneSchemaValue(nested)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = cloneSchemaValue(nested)
		}
		return out
	default:
		return value
	}
}
