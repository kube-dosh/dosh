import { describe, expect, it } from "vitest";
import type { ResourceSchema } from "@/api/types";
import {
  defaultValueForSchema, resolveSchemaNode, setObjectPath, validateResourceObject,
} from "@/features/resources/resource-schema";

const schema: ResourceSchema = {
  group: "example.io", version: "v1", resource: "widgets", kind: "Widget", source: "openapi-v2",
  schema: {
    type: "object",
    required: ["spec"],
    properties: {
      metadata: { $ref: "#/definitions/ObjectMeta" },
      spec: { $ref: "#/definitions/WidgetSpec" },
    },
  },
  definitions: {
    ObjectMeta: { type: "object", required: ["name"], properties: { name: { type: "string" }, namespace: { type: "string" } } },
    WidgetSpec: { type: "object", required: ["size"], properties: { size: { type: "string", enum: ["small", "large"] }, replicas: { type: "integer" } } },
  },
};

describe("resource schema helpers", () => {
  it("resolves refs and allOf schemas", () => {
    const resolved = resolveSchemaNode({
      allOf: [
        { type: "object", required: ["name"], properties: { name: { type: "string" } } },
        { $ref: "#/definitions/Extra" },
      ],
    }, { Extra: { type: "object", properties: { enabled: { type: "boolean" } } } });
    expect(resolved.type).toBe("object");
    expect(resolved.required).toEqual(["name"]);
    expect(resolved.properties).toMatchObject({ name: { type: "string" }, enabled: { type: "boolean" } });
  });

  it("validates required fields and primitive types without mutating the object", () => {
    const object = { metadata: { name: "sample" }, spec: { size: "large", replicas: 2 } };
    expect(validateResourceObject(schema, object)).toEqual([]);
    expect(validateResourceObject(schema, { metadata: { name: "sample" }, spec: { replicas: "two" } })).toEqual([
      { path: "$.spec.replicas", message: "类型应为 integer" },
      { path: "$.spec.size", message: "缺少必填字段" },
    ]);
    expect(object.spec.size).toBe("large");
  });

  it("sets nested fields immutably and supplies safe defaults", () => {
    const source = { metadata: { name: "sample" }, spec: {} };
    const next = setObjectPath(source, ["spec", "replicas"], 3);
    expect(next).toEqual({ metadata: { name: "sample" }, spec: { replicas: 3 } });
    expect(source).toEqual({ metadata: { name: "sample" }, spec: {} });
    expect(defaultValueForSchema({ type: "object" })).toEqual({});
    expect(defaultValueForSchema({ type: "array" })).toEqual([]);
    expect(defaultValueForSchema({ type: "integer" })).toBe(0);
  });
});
