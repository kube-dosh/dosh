import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import { Input } from "@/components/ui/input";
import {
  SearchableSelect,
  type SearchableSelectOption,
} from "@/components/ui/searchable-select";

export interface NamespacePlan {
  exists: boolean;
  recommended: boolean;
  dangerous: boolean;
  createRequired: boolean;
  loading: boolean;
  unavailable: boolean;
}

function isDangerousNamespace(name: string) {
  return name === "default" || name === "kube-system" || name.startsWith("kube-");
}

export function NamespacePicker({
  id = "namespace-picker",
  label = "命名空间",
  value,
  recommended,
  onChange,
  onPlanChange,
  disabled = false,
}: {
  id?: string;
  label?: string;
  value: string;
  recommended?: string;
  onChange: (value: string) => void;
  onPlanChange?: (plan: NamespacePlan) => void;
  disabled?: boolean;
}) {
  const [manual, setManual] = useState(false);
  const lastPlanSignature = useRef("");
  const query = useQuery({
    queryKey: ["namespaces", "picker"],
    queryFn: () => api.namespaces({ pageSize: 200 }),
    staleTime: 30_000,
    retry: false,
  });

  const namespaces = useMemo(
    () => (query.data?.items ?? []).filter((item) => item.status !== "Terminating"),
    [query.data?.items],
  );
  const existingNames = useMemo(
    () => new Set(namespaces.map((item) => item.name)),
    [namespaces],
  );
  const trimmedValue = value.trim();
  const recommendedValue = recommended?.trim() ?? "";
  const loading = query.isLoading;
  const unavailable = query.isError;
  const exists = Boolean(trimmedValue && existingNames.has(trimmedValue));
  const recommendedSelected = Boolean(
    recommendedValue && trimmedValue === recommendedValue,
  );
  const dangerous = isDangerousNamespace(trimmedValue);
  const createRequired = Boolean(
    trimmedValue && !exists && !loading && !unavailable,
  );

  useEffect(() => {
    if (unavailable) setManual(true);
  }, [unavailable]);

  useEffect(() => {
    const plan: NamespacePlan = {
      exists,
      recommended: recommendedSelected,
      dangerous,
      createRequired,
      loading,
      unavailable,
    };
    const signature = [
      plan.exists,
      plan.recommended,
      plan.dangerous,
      plan.createRequired,
      plan.loading,
      plan.unavailable,
    ].join(":");
    if (lastPlanSignature.current === signature) return;
    lastPlanSignature.current = signature;
    onPlanChange?.(plan);
  }, [
    createRequired,
    dangerous,
    exists,
    loading,
    onPlanChange,
    recommendedSelected,
    unavailable,
  ]);

  const options = useMemo(() => {
    const result: SearchableSelectOption[] = [];
    const add = (option: SearchableSelectOption) => {
      if (!result.some((item) => item.value === option.value)) result.push(option);
    };
    if (recommendedValue && !existingNames.has(recommendedValue)) {
      add({
        value: recommendedValue,
        label: `${recommendedValue}（推荐，将新建）`,
      });
    }
    if (
      trimmedValue &&
      trimmedValue !== recommendedValue &&
      !existingNames.has(trimmedValue) &&
      !loading &&
      !unavailable
    ) {
      add({
        value: trimmedValue,
        label: `${trimmedValue}（手动输入，将新建）`,
      });
    }
    for (const item of namespaces) {
      add({
        value: item.name,
        label:
          item.name === recommendedValue
            ? `${item.name}（推荐）`
            : item.name,
      });
    }
    return result;
  }, [
    existingNames,
    loading,
    namespaces,
    recommendedValue,
    trimmedValue,
    unavailable,
  ]);

  const hints: Array<{ key: string; tone: "muted" | "warning"; text: string }> = [];
  if (!trimmedValue) {
    hints.push({ key: "empty", tone: "muted", text: "请选择已有命名空间，或手动输入新名称。" });
  }
  if (unavailable) {
    hints.push({
      key: "unavailable",
      tone: "warning",
      text: "无法读取命名空间列表，已切换为手动输入；可在 Helm 高级执行选项中确认是否创建命名空间。",
    });
  } else if (loading) {
    hints.push({ key: "loading", tone: "muted", text: "正在读取集群命名空间…" });
  } else if (createRequired) {
    hints.push({
      key: "create",
      tone: "warning",
      text: `命名空间 ${trimmedValue} 不存在，安装时将显式创建。`,
    });
  }
  if (dangerous) {
    hints.push({
      key: "dangerous",
      tone: "warning",
      text: "default、kube-system 与 kube-* 属于系统命名空间，安装第三方工作负载可能带来集群级风险。",
    });
  }

  return (
    <div className="min-w-0 space-y-1.5">
      {manual || unavailable ? (
        <>
          <Input
            id={id}
            aria-label={label}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            placeholder="输入命名空间名称"
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            disabled={disabled}
          />
          {!unavailable ? (
            <button
              type="button"
              className="flex items-center gap-1 text-xs text-primary hover:underline disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => setManual(false)}
              disabled={disabled}
            >
              <ArrowLeft className="h-3 w-3" />
              返回命名空间列表
            </button>
          ) : null}
        </>
      ) : (
        <SearchableSelect
          id={id}
          label={label}
          value={trimmedValue}
          options={options}
          onChange={onChange}
          onManualSelect={() => setManual(true)}
          placeholder={loading ? "正在加载命名空间…" : "搜索并选择命名空间"}
          searchPlaceholder="搜索命名空间…"
          disabled={disabled || loading}
        />
      )}
      {hints.map((hint) => (
        <p
          key={hint.key}
          className={
            hint.tone === "warning"
              ? "flex min-w-0 items-start gap-1.5 break-words text-xs text-warning"
              : "break-words text-xs text-muted-foreground"
          }
        >
          {hint.tone === "warning" ? (
            <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          ) : null}
          <span className="min-w-0">{hint.text}</span>
        </p>
      ))}
    </div>
  );
}
