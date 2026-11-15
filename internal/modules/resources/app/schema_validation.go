package app

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"dosh/internal/domain"
)

// validateResourceObject performs a bounded, schema-aware preflight for clear
// required-field and primitive type errors. It deliberately does not attempt to
// implement every OpenAPI keyword; Kubernetes API Server dry-run stays
// authoritative for admission, defaults, CEL and structural schema rules.
func validateResourceObject(schema domain.ResourceSchema, object map[string]any) []string {
	if len(schema.Schema) == 0 {
		return nil
	}
	resolved := resolveResourceSchema(schema.Schema, schema.Definitions, 0)
	problems := make([]string, 0, 4)
	validateSchemaValue("$", object, resolved, schema.Definitions, 0, &problems)
	sort.Strings(problems)
	return problems
}

func validateSchemaValue(path string, value any, schema map[string]any, definitions map[string]map[string]any, depth int, problems *[]string) {
	if len(schema) == 0 || depth > 10 {
		return
	}
	if value == nil {
		if nullable, _ := schema["nullable"].(bool); !nullable {
			appendSchemaProblem(problems, path, "不能为 null")
		}
		return
	}
	if allowed, ok := schema["x-kubernetes-int-or-string"].(bool); ok && allowed {
		if _, isString := value.(string); isString {
			return
		}
		if isNumber(value) {
			return
		}
		appendSchemaProblem(problems, path, "应为整数或字符串")
		return
	}
	types := schemaTypes(schema["type"])
	if len(types) > 0 && !matchesSchemaType(value, types) {
		appendSchemaProblem(problems, path, fmt.Sprintf("类型应为 %s", strings.Join(types, " 或 ")))
		return
	}
	if isObject(value) {
		object, _ := value.(map[string]any)
		for _, required := range schemaRequired(schema["required"]) {
			if _, exists := object[required]; !exists {
				appendSchemaProblem(problems, path+"."+required, "缺少必填字段")
			}
		}
		properties := schemaObjectMap(schema["properties"])
		for name, nestedValue := range object {
			nested, ok := properties[name].(map[string]any)
			if !ok {
				continue
			}
			validateSchemaValue(path+"."+name, nestedValue, resolveResourceSchema(nested, definitions, depth+1), definitions, depth+1, problems)
		}
	}
	if isArray(value) {
		items, _ := value.([]any)
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range items {
				validateSchemaValue(fmt.Sprintf("%s[%d]", path, i), item, resolveResourceSchema(itemSchema, definitions, depth+1), definitions, depth+1, problems)
			}
		}
	}
}

func resolveResourceSchema(schema map[string]any, definitions map[string]map[string]any, depth int) map[string]any {
	if len(schema) == 0 || depth > 10 {
		return schema
	}
	if ref, ok := schema["$ref"].(string); ok {
		key := strings.TrimPrefix(ref, "#/definitions/")
		if target := definitions[key]; len(target) > 0 {
			return resolveResourceSchema(target, definitions, depth+1)
		}
	}
	allOf, _ := schema["allOf"].([]any)
	if len(allOf) == 0 {
		return schema
	}
	merged := make(map[string]any, len(schema)+1)
	for key, value := range schema {
		if key != "allOf" {
			merged[key] = value
		}
	}
	properties := schemaObjectMap(merged["properties"])
	required := append([]string(nil), schemaRequired(merged["required"])...)
	for _, raw := range allOf {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resolved := resolveResourceSchema(part, definitions, depth+1)
		for name, value := range schemaObjectMap(resolved["properties"]) {
			properties[name] = value
		}
		required = append(required, schemaRequired(resolved["required"])...)
		if _, exists := merged["type"]; !exists && resolved["type"] != nil {
			merged["type"] = resolved["type"]
		}
	}
	if len(properties) > 0 {
		merged["properties"] = properties
	}
	if len(required) > 0 {
		merged["required"] = uniqueStrings(required)
	}
	return merged
}

func schemaTypes(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			return []string{typed}
		}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func matchesSchemaType(value any, types []string) bool {
	for _, candidate := range types {
		switch candidate {
		case "object":
			if isObject(value) {
				return true
			}
		case "array":
			if isArray(value) {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "integer":
			if number, ok := numericValue(value); ok && math.Trunc(number) == number {
				return true
			}
		case "number":
			if _, ok := numericValue(value); ok {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return false
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func isNumber(value any) bool {
	_, ok := numericValue(value)
	return ok
}

func isObject(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func isArray(value any) bool {
	_, ok := value.([]any)
	return ok
}

func schemaObjectMap(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func schemaRequired(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendSchemaProblem(problems *[]string, path, message string) {
	*problems = append(*problems, fmt.Sprintf("%s %s", path, message))
}
