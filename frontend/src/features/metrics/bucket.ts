import { useI18n } from "vue-i18n";
import { useFormat } from "@/shared/i18n";
import type { HistoryPoint } from "./catalog";

/** Labels of one history time bucket, shared by the chart readout and the data table. */
export function useBucketText() {
  const { t } = useI18n();
  const format = useFormat();
  return {
    time(seconds: number | undefined): string {
      if (seconds === undefined) return "—";
      return format.dateTime(new Date(seconds * 1000), {
        dateStyle: "medium",
        timeStyle: "short",
      });
    },
    coverage(point: HistoryPoint): string {
      if (point.partial) return t("metrics.history.coverage.partial");
      if (point.incomplete) return t("metrics.history.coverage.incomplete");
      return t("metrics.history.coverage.complete");
    },
  };
}
