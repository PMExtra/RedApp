export type ProxyConfig = { mode: "inherit" | "direct" | "url"; url?: string };
// The server shows saved proxy passwords as this placeholder and keeps the saved
// password when it is submitted for the same scheme, user and host.
export const REDACTED_PROXY_PASSWORD = "****";
export interface ProxyEffective extends ProxyConfig {
  source_scope: "app" | "vendor" | "global";
  source_id: string;
  dns: string;
}
export interface Configuration {
  revision: number;
  instructions_revision?: number;
  template_ref: string | null;
  template_hash: string | null;
  template_missing: boolean;
  defaults: Record<string, unknown> | null;
  overrides: Record<string, unknown>;
  effective: Record<string, unknown>;
  fields: Record<
    string,
    { source: "inherited" | "custom"; differs_from_template: boolean | null }
  >;
  proxy_effective: ProxyEffective;
}
export function getLeaf(value: unknown, path: string): unknown {
  return path
    .split(".")
    .reduce<unknown>(
      (current, key) => (current as Record<string, unknown> | undefined)?.[key],
      value,
    );
}
export function setLeaf(value: object, path: string, next: unknown) {
  const keys = path.split(".");
  let current = value as Record<string, unknown>;
  for (const key of keys.slice(0, -1))
    current = current[key] as Record<string, unknown>;
  current[keys.at(-1)!] = JSON.parse(JSON.stringify(next));
}
