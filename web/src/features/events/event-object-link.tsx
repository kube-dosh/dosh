import { Link } from "@tanstack/react-router";
import { ArrowUpRight } from "lucide-react";
import type { EventItem } from "@/api/types";

const linkClassName = "inline-flex max-w-full items-center gap-1 font-mono text-xs font-medium text-primary underline-offset-2 hover:underline";

const workloadKinds = new Set(["deployment", "statefulset", "daemonset", "job", "cronjob"]);

export function EventObjectLink({ event }: { event: EventItem }) {
  const kind = event.objectKind.toLowerCase();
  const label = `${event.objectKind}/${event.objectName}`;
  const content = <>{label}<ArrowUpRight className="h-3 w-3 shrink-0" aria-hidden="true" /></>;

  if (kind === "pod") {
    return <Link to="/pods/$namespace/$name" params={{ namespace: event.namespace, name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (workloadKinds.has(kind)) {
    return <Link to="/workloads/$kind/$namespace/$name" params={{ kind, namespace: event.namespace, name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "service") {
    return <Link to="/network/services/$namespace/$name" params={{ namespace: event.namespace, name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "ingress") {
    return <Link to="/network/ingresses/$namespace/$name" params={{ namespace: event.namespace, name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "node") {
    return <Link to="/nodes/$name" params={{ name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "namespace") {
    return <Link to="/namespaces/$name" params={{ name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "persistentvolumeclaim") {
    return <Link to="/storage/claims/$namespace/$name" params={{ namespace: event.namespace, name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "persistentvolume") {
    return <Link to="/storage/volumes/$name" params={{ name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "storageclass") {
    return <Link to="/storage/classes/$name" params={{ name: event.objectName }} search={{}} className={linkClassName}>{content}</Link>;
  }
  if (kind === "configmap" || kind === "secret") {
    return <Link to="/configuration" search={{ tab: kind === "secret" ? "secrets" : "configmaps", namespace: event.namespace, q: event.objectName }} className={linkClassName}>{content}</Link>;
  }
  return <span className="font-mono text-xs text-muted-foreground">{label}</span>;
}
