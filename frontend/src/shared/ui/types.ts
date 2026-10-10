// Public prop types of the UI components. They live in a .ts file so that
// TypeScript files (and type-aware lint rules) see them without the .vue
// compiler.
import type { Component } from "vue";
import type { RouteLocationRaw } from "vue-router";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md";
export type BadgeTone = "neutral" | "primary" | "success" | "warning" | "danger" | "info";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface ComboboxOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface RadioOption {
  value: string;
  label: string;
  description?: string;
  disabled?: boolean;
}

export interface TabItem {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface NavTabItem {
  to: RouteLocationRaw;
  label: string;
}

export interface Crumb {
  label: string;
  /** Omit for the current page (always the last crumb). */
  to?: RouteLocationRaw;
}

export interface SideNavItem {
  label: string;
  to: RouteLocationRaw;
  icon?: Component;
  /** Highlight only on an exact match (default: also on nested routes). */
  exact?: boolean;
}

export interface SideNavSection {
  label?: string;
  items: SideNavItem[];
}

export interface DataTableColumn {
  key: string;
  label: string;
  /** Server-side sort key; the column header becomes a sort button. */
  sortable?: boolean;
  align?: "start" | "end" | "center";
  /** Visually hide the header text (keep it for screen readers). */
  hideLabel?: boolean;
  class?: string;
}

export interface DataTableSort {
  key: string;
  direction: "asc" | "desc";
}

export interface FieldControlProps {
  id: string;
  "aria-describedby": string | undefined;
  "aria-invalid": "true" | undefined;
  "aria-required": "true" | undefined;
}

export interface FormFieldBinding extends FieldControlProps {
  name: string;
  // The schema types the value; controls declare their own model type.
  /* eslint-disable @typescript-eslint/no-explicit-any */
  modelValue: any;
  "onUpdate:modelValue": (value: any) => void;
  /* eslint-enable @typescript-eslint/no-explicit-any */
  onBlur: () => void;
}
