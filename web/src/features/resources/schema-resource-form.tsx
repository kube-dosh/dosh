import { useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { parse, stringify } from "yaml";
import type { ResourceSchema } from "@/api/types";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  defaultValueForSchema, isRecord, resolveSchemaNode, schemaEnum, schemaProperties,
  schemaRequired, schemaType, setObjectPath, type ResourceObject, type SchemaIssue, type SchemaNode,
} from "@/features/resources/resource-schema";

export function SchemaResourceForm({ schema, object, namespaced, issues, disabled, onChange }: {
  schema?: ResourceSchema;
  object: ResourceObject;
  namespaced: boolean;
  issues: SchemaIssue[];
  disabled?: boolean;
  onChange: (object: ResourceObject) => void;
}) {
  const definitions = schema?.definitions as Record<string, SchemaNode> | undefined;
  const root = useMemo(() => resolveSchemaNode(schema?.schema as SchemaNode | undefined, definitions), [definitions, schema?.schema]);
  const properties = schemaProperties(root.properties);
  if (!schema || schema.source !== "openapi-v2" || Object.keys(properties).length === 0) {
    return (
      <div className="rounded-md border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
        当前资源类型没有可用的 OpenAPI Schema。请切换到 YAML 模式编辑；API Server Dry Run 仍是最终校验依据。
      </div>
    );
  }
  const metadata = isRecord(object.metadata) ? object.metadata : {};
  const setMetadata = (key: string, value: unknown) => onChange(setObjectPath(object, ["metadata", key], value));
  const rootNames = Object.keys(properties).filter((name) => !["apiVersion", "kind", "metadata", "status"].includes(name));
  return (
    <div className="min-w-0 space-y-5">
      <section className="min-w-0 rounded-lg border p-3 sm:p-4" aria-labelledby="schema-form-identity">
        <h3 id="schema-form-identity" className="mb-3 text-sm font-semibold">基本信息</h3>
        <div className="grid min-w-0 gap-4 sm:grid-cols-2">
          <SchemaTextInput
            path="metadata.name"
            label="名称"
            required
            value={typeof metadata.name === "string" ? metadata.name : ""}
            issue={issues.find((item) => item.path === "$.metadata.name")}
            disabled={disabled}
            onChange={(value) => setMetadata("name", value)}
          />
          {namespaced ? (
            <SchemaTextInput
              path="metadata.namespace"
              label="命名空间"
              required
              value={typeof metadata.namespace === "string" ? metadata.namespace : ""}
              issue={issues.find((item) => item.path === "$.metadata.namespace")}
              disabled={disabled}
              onChange={(value) => setMetadata("namespace", value)}
            />
          ) : null}
        </div>
        <dl className="mt-4 grid gap-3 text-xs sm:grid-cols-2">
          <div className="min-w-0"><dt className="text-muted-foreground">apiVersion</dt><dd className="break-all font-mono">{String(object.apiVersion ?? "")}</dd></div>
          <div className="min-w-0"><dt className="text-muted-foreground">kind</dt><dd className="break-all font-mono">{String(object.kind ?? "")}</dd></div>
        </dl>
      </section>
      {rootNames.length ? (
        <section className="min-w-0 rounded-lg border p-3 sm:p-4" aria-labelledby="schema-form-fields">
          <h3 id="schema-form-fields" className="mb-3 text-sm font-semibold">资源字段</h3>
          <div className="min-w-0 space-y-4">
            {rootNames.map((name) => (
              <SchemaField
                key={name}
                name={name}
                path={["$", name]}
                schema={properties[name]}
                value={object[name]}
                required={schemaRequired(root.required).includes(name)}
                definitions={definitions}
                issues={issues}
                disabled={disabled}
                depth={0}
                onChange={(value) => onChange(setObjectPath(object, [name], value))}
              />
            ))}
          </div>
          <OptionalFieldAdder
            properties={properties}
            present={rootNames}
            disabled={disabled}
            onAdd={(name) => onChange(setObjectPath(object, [name], defaultValueForSchema(properties[name], definitions)))}
          />
        </section>
      ) : null}
    </div>
  );
}

function SchemaField({ name, path, schema, value, required, definitions, issues, disabled, depth, onChange }: {
  name: string;
  path: string[];
  schema: SchemaNode;
  value: unknown;
  required: boolean;
  definitions?: Record<string, SchemaNode>;
  issues: SchemaIssue[];
  disabled?: boolean;
  depth: number;
  onChange: (value: unknown) => void;
}) {
  const resolved = useMemo(() => resolveSchemaNode(schema, definitions), [definitions, schema]);
  const type = schemaType(resolved);
  const pathText = path.join(".").replace("$.", "");
  const issue = issues.find((item) => item.path === path.join("."));
  const label = fieldLabel(name, path, resolved);

  if (type === "object") {
    const nestedProperties = schemaProperties(resolved.properties);
    const nestedValue = isRecord(value) ? value : {};
    const visible = Object.keys(nestedProperties).filter((key) => schemaRequired(resolved.required).includes(key) || key in nestedValue);
    return (
      <fieldset className="min-w-0 rounded-md border bg-muted/10 p-3" aria-describedby={issue ? `${fieldID(path)}-error` : undefined}>
        <legend className="px-1 text-sm font-medium">{label}{required ? <span className="ml-1 text-destructive-strong" aria-hidden="true">*</span> : null}</legend>
        {resolved.description ? <p className="mb-3 text-xs leading-5 text-muted-foreground">{String(resolved.description)}</p> : null}
        {Object.keys(nestedProperties).length === 0 ? (
          <p className="text-xs text-muted-foreground">该对象由服务器定义，当前 Schema 未暴露固定字段；可切换到 YAML 模式补充。</p>
        ) : (
          <div className="min-w-0 space-y-3">
            {visible.map((key) => (
              <SchemaField
                key={key}
                name={key}
                path={[...path, key]}
                schema={nestedProperties[key]}
                value={nestedValue[key]}
                required={schemaRequired(resolved.required).includes(key)}
                definitions={definitions}
                issues={issues}
                disabled={disabled}
                depth={depth + 1}
                onChange={(nested) => onChange(setObjectPath(nestedValue, [key], nested))}
              />
            ))}
          </div>
        )}
        <OptionalFieldAdder
          properties={nestedProperties}
          present={visible}
          disabled={disabled}
          onAdd={(key) => onChange(setObjectPath(nestedValue, [key], defaultValueForSchema(nestedProperties[key], definitions)))}
        />
        <SchemaIssueText path={path} issue={issue} />
      </fieldset>
    );
  }
  if (type === "array" || type === "int-or-string") {
    return (
      <ArrayOrFreeformField
        path={path}
        label={label}
        required={required}
        value={value}
        issue={issue}
        disabled={disabled}
        description={resolved.description}
        onChange={onChange}
      />
    );
  }
  if (type === "boolean") {
    const checked = value === true;
    return (
      <div className="min-w-0 space-y-1.5">
        <label className="flex min-w-0 items-center gap-2 text-sm font-medium">
          <input
            id={fieldID(path)}
            type="checkbox"
            className="h-4 w-4 shrink-0 rounded border-input accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            checked={checked}
            disabled={disabled}
            aria-invalid={Boolean(issue)}
            onChange={(event) => onChange(event.target.checked)}
          />
          <span className="min-w-0 break-words">{label}{required ? <span className="ml-1 text-destructive-strong" aria-hidden="true">*</span> : null}</span>
        </label>
        <SchemaDescription description={resolved.description} />
        <SchemaIssueText path={path} issue={issue} />
      </div>
    );
  }
  if (type === "number" || type === "integer") {
    return (
      <div className="min-w-0 space-y-1.5">
        <FieldLabel path={path} label={label} required={required} />
        <Input
          id={fieldID(path)}
          type="number"
          step={type === "integer" ? "1" : "any"}
          value={typeof value === "number" ? String(value) : ""}
          disabled={disabled}
          aria-invalid={Boolean(issue)}
          aria-describedby={issue ? `${fieldID(path)}-error` : undefined}
          onChange={(event) => onChange(event.target.value === "" ? undefined : Number(event.target.value))}
        />
        <SchemaDescription description={resolved.description} />
        <SchemaIssueText path={path} issue={issue} />
      </div>
    );
  }
  const options = schemaEnum(resolved);
  if (options.length) {
    return (
      <div className="min-w-0 space-y-1.5">
        <FieldLabel path={path} label={label} required={required} />
        <select
          id={fieldID(path)}
          className="flex h-9 w-full min-w-0 rounded-md border border-input bg-card px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          value={typeof value === "string" ? value : ""}
          disabled={disabled}
          aria-invalid={Boolean(issue)}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">请选择</option>
          {options.map((option) => <option key={option} value={option}>{option}</option>)}
        </select>
        <SchemaDescription description={resolved.description} />
        <SchemaIssueText path={path} issue={issue} />
      </div>
    );
  }
  return (
    <SchemaTextInput
      path={pathText}
      label={label}
      required={required}
      value={typeof value === "string" ? value : value === undefined ? "" : String(value)}
      issue={issue}
      description={typeof resolved.description === "string" ? resolved.description : undefined}
      disabled={disabled}
      onChange={(next) => onChange(next)}
    />
  );
}

function ArrayOrFreeformField({ path, label, required, value, issue, disabled, description, onChange }: {
  path: string[];
  label: string;
  required: boolean;
  value: unknown;
  issue?: SchemaIssue;
  disabled?: boolean;
  description: unknown;
  onChange: (value: unknown) => void;
}) {
  const [text, setText] = useState(() => formatUnknownForEditor(value));
  const [parseError, setParseError] = useState("");
  const id = fieldID(path);
  const update = (next: string) => {
    setText(next);
    if (!next.trim()) {
      setParseError("");
      onChange(undefined);
      return;
    }
    try {
      const parsed = parse(next);
      setParseError("");
      onChange(parsed);
    } catch (error) {
      setParseError(error instanceof Error ? error.message : "YAML 解析失败");
    }
  };
  return (
    <div className="min-w-0 space-y-1.5">
      <FieldLabel path={path} label={label} required={required} />
      <Textarea
        id={id}
        value={text}
        disabled={disabled}
        rows={4}
        className="min-h-24 min-w-0 font-mono text-xs"
        aria-invalid={Boolean(issue || parseError)}
        aria-describedby={issue || parseError ? `${id}-error` : undefined}
        onChange={(event) => update(event.target.value)}
      />
      <p className="text-xs text-muted-foreground">数组或自由格式值使用 YAML/JSON 语法；留空表示移除。</p>
      <SchemaDescription description={description} />
      {parseError ? <p id={`${id}-error`} role="alert" className="text-xs text-destructive-strong">{parseError}</p> : <SchemaIssueText path={path} issue={issue} />}
    </div>
  );
}

function OptionalFieldAdder({ properties, present, disabled, onAdd }: {
  properties: Record<string, SchemaNode>;
  present: string[];
  disabled?: boolean;
  onAdd: (name: string) => void;
}) {
  const available = Object.keys(properties).filter((name) => !present.includes(name) && !["apiVersion", "kind", "metadata", "status"].includes(name));
  if (!available.length) return null;
  return (
    <div className="mt-3 flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
      <Select value="" disabled={disabled} onValueChange={onAdd}>
        <SelectTrigger className="h-8 w-full min-w-0 text-xs sm:w-64" aria-label="添加可选字段"><Plus className="mr-2 h-3.5 w-3.5 shrink-0" /><SelectValue placeholder="添加可选字段" /></SelectTrigger>
        <SelectContent>
          {available.map((name) => <SelectItem key={name} value={name}>{name}</SelectItem>)}
        </SelectContent>
      </Select>
      <span className="text-xs text-muted-foreground">仅列出当前 Schema 定义的可选字段</span>
    </div>
  );
}

function SchemaTextInput({ path, label, required, value, issue, description, disabled, onChange }: {
  path: string;
  label: string;
  required?: boolean;
  value: string;
  issue?: SchemaIssue;
  description?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const id = fieldID(path.split("."));
  return (
    <div className="min-w-0 space-y-1.5">
      <Label htmlFor={id} className="break-words">{label}{required ? <span className="ml-1 text-destructive-strong" aria-hidden="true">*</span> : null}</Label>
      <Input id={id} value={value} disabled={disabled} aria-invalid={Boolean(issue)} aria-describedby={issue ? `${id}-error` : undefined} onChange={(event) => onChange(event.target.value)} />
      {description ? <p className="text-xs leading-5 text-muted-foreground">{description}</p> : null}
      <SchemaIssueText path={path.split(".")} issue={issue} />
    </div>
  );
}

function FieldLabel({ path, label, required }: { path: string[]; label: string; required: boolean }) {
  return <Label htmlFor={fieldID(path)} className="break-words">{label}{required ? <span className="ml-1 text-destructive-strong" aria-hidden="true">*</span> : null}</Label>;
}

function SchemaDescription({ description }: { description: unknown }) {
  if (typeof description !== "string" || !description) return null;
  return <p className="text-xs leading-5 text-muted-foreground">{description}</p>;
}

function SchemaIssueText({ path, issue }: { path: string[]; issue?: SchemaIssue }) {
  if (!issue) return null;
  return <p id={`${fieldID(path)}-error`} role="alert" className="text-xs text-destructive-strong">{issue.path} {issue.message}</p>;
}

function fieldID(path: string[]) {
  return `schema-field-${path.map((part) => part.replace(/[^a-zA-Z0-9_-]/g, "-")).join("-")}`;
}

function fieldLabel(name: string, path: string[], schema: SchemaNode): string {
  if (typeof schema.title === "string" && schema.title.trim()) return schema.title;
  if (path.length <= 2) return name;
  return name;
}

function formatUnknownForEditor(value: unknown): string {
  if (value === undefined) return "";
  try {
    return stringify(value, { lineWidth: 0 }).trimEnd();
  } catch {
    return String(value);
  }
}

export function schemaIssueSummary(issues: SchemaIssue[]): string {
  return issues.slice(0, 4).map((issue) => `${issue.path} ${issue.message}`).join("；") + (issues.length > 4 ? `；另有 ${issues.length - 4} 项` : "");
}
