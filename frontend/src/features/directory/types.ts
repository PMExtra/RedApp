import type { Schema } from "@/shared/api";

/** Props of `FieldReset` for one overlay path (see `useOverlayForm().resetBinding`). */
export interface ResetBinding {
  linked: boolean;
  origin?: Schema<"FieldOrigin">;
  modified?: boolean;
  templateValue?: string;
}
