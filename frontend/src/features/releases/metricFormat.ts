import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import type { Metric } from "./queries";

/** Value and name formatting for application metrics. */
export function useMetricFormat() {
  const i18n = useI18n();
  const { t } = i18n;
  const format = useFormat();

  function value(amount: number | null | undefined, unit: Metric["unit"]): string {
    if (amount === null || amount === undefined) return t("common.states.unknown");
    switch (unit) {
      case "bytes":
        return format.bytes(amount);
      case "bytes_per_second":
        return t("releases.metrics.perSecond", { value: format.bytes(amount) });
      case "seconds":
        return format.duration(Math.round(amount));
      default:
        return format.number(amount);
    }
  }

  function name(metric: Pick<Metric, "key" | "label">): string {
    const key = `releases.metrics.names.${metric.key}`;
    return i18n.te(key) ? t(key) : metric.label;
  }

  return { value, name };
}
