import type { ResourceSchema } from "@/api/types";

export type SchemaNode = Record<string, unknown>;
export type ResourceObject = Record<string, unknown>;

export interface SchemaIssue {
  path: string;
  message: string;
}

export function resolveSchemaNode(node: SchemaNode | undefined, definitions: Record<string, SchemaNode> | undefined, depth = 0): SchemaNode {
  if (!node || depth > 10) return {};
  if (typeof node.$ref === "string") {
    const key = node.$ref.replace("#/definitions/", "");
    const target = definitions?.[key];
    if (target) return resolveSchemaNode(target, definitions, depth + 1);
  }
  if (!Array.isArray(node.allOf)) return node;
  const merged: SchemaNode = { ...node };
  delete merged.allOf;
  const properties = schemaProperties(merged.properties);
  const required = new Set(schemaRequired(merged.required));
  for (const part of node.allOf) {
    if (!isRecord(part)) continue;
    const resolved = resolveSchemaNode(part, definitions, depth + 1);
    Object.assign(properties, schemaProperties(resolved.properties));
    for (const item of schemaRequired(resolved.required)) required.add(item);
    if (merged.type === undefined && resolved.type !== undefined) merged.type = resolved.type;
  }
  if (Object.keys(properties).length) merged.properties = properties;
  if (required.size) merged.required = [...required];
  return merged;
}

export function schemaProperties(value: unknown): Record<string, SchemaNode> {
  if (!isRecord(value)) return {};
  const out: Record<string, SchemaNode> = {};
  for (const [key, nested] of Object.entries(value)) {
    if (isRecord(nested)) out[key] = nested;
  }
  return out;
}

export function schemaRequired(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string");
}

export function schemaType(node: SchemaNode): string {
  if (node["x-kubernetes-int-or-string"] === true) return "int-or-string";
  const value = node.type;
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    const first = value.find((item): item is string => typeof item === "string" && item !== "null");
    if (first) return first;
  }
  if (isRecord(node.properties)) return "object";
  if (node.items) return "array";
  return "string";
}

export function schemaEnum(node: SchemaNode): string[] {
  if (!Array.isArray(node.enum)) return [];
  return node.enum.filter((item): item is string => typeof item === "string");
}

export function getObjectPath(object: ResourceObject, path: string[]): unknown {
  let current: unknown = object;
  for (const part of path) {
    if (!isRecord(current)) return undefined;
    current = current[part];
  }
  return current;
}

export function setObjectPath(object: ResourceObject, path: string[], value: unknown): ResourceObject {
  if (!path.length) return object;
  const root: ResourceObject = { ...object };
  let current = root;
  for (let index = 0; index < path.length - 1; index++) {
    const part = path[index];
    const next = isRecord(current[part]) ? { ...(current[part] as ResourceObject) } : {};
    current[part] = next;
    current = next;
  }
  const last = path[path.length - 1];
  if (value === undefined) delete current[last];
  else current[last] = value;
  return root;
}

export function defaultValueForSchema(node: SchemaNode, definitions?: Record<string, SchemaNode>): unknown {
  const resolved = resolveSchemaNode(node, definitions);
  switch (schemaType(resolved)) {
    case "object": return {};
    case "array": return [];
    case "boolean": return false;
    case "integer":
    case "number": return 0;
    default: return "";
  }
}

export function validateResourceObject(schema: ResourceSchema, object: ResourceObject): SchemaIssue[] {
  if (!schema.schema || schema.source !== "openapi-v2") return [];
  const issues: SchemaIssue[] = [];
  validateValue("$", object, resolveSchemaNode(schema.schema as SchemaNode, schema.definitions), schema.definitions, 0, issues);
  return issues.sort((left, right) => left.path.localeCompare(right.path));
}

function validateValue(path: string, value: unknown, schema: SchemaNode, definitions: Record<string, SchemaNode> | undefined, depth: number, issues: SchemaIssue[]) {
  if (!schema || depth > 10) return;
  if (value === null) {
    if (schema.nullable !== true) issues.push({ path, message: "不能为 null" });
    return;
  }
  const type = schemaType(schema);
  if (type === "int-or-string") {
    if (typeof value !== "string" && typeof value !== "number") issues.push({ path, message: "应为整数或字符串" });
    return;
  }
  const typeMatches = type === "object" ? isRecord(value)
    : type === "array" ? Array.isArray(value)
      : type === "integer" ? typeof value === "number" && Number.isInteger(value)
        : type === "number" ? typeof value === "number"
          : type === "boolean" ? typeof value === "boolean"
            : typeof value === "string";
  if (!typeMatches) {
    issues.push({ path, message: `类型应为 ${type}` });
    return;
  }
  if (isRecord(value)) {
    for (const required of schemaRequired(schema.required)) {
      if (!(required in value)) issues.push({ path: `${path}.${required}`, message: "缺少必填字段" });
    }
    for (const [name, nestedValue] of Object.entries(value)) {
      const nested = schemaProperties(schema.properties)[name];
      if (nested) validateValue(`${path}.${name}`, nestedValue, resolveSchemaNode(nested, definitions), definitions, depth + 1, issues);
    }
  }
  if (Array.isArray(value) && isRecord(schema.items)) {
    value.forEach((item, index) => validateValue(`${path}[${index}]`, item, resolveSchemaNode(schema.items as SchemaNode, definitions), definitions, depth + 1, issues));
  }
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
