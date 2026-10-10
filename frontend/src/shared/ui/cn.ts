type ClassValue = string | false | null | undefined;

/** Joins class names, skipping falsy values. */
export function cn(...values: ClassValue[]): string {
  return values.filter(Boolean).join(" ");
}

/** Shared control styling for text-like inputs. */
export const controlClass =
  "w-full rounded-md border border-border-strong bg-surface px-3 text-sm text-fg shadow-sm " +
  "placeholder:text-subtle focus-ring disabled:cursor-not-allowed disabled:opacity-60 " +
  "aria-[invalid=true]:border-danger";

/** Floating panels (menus, popovers, listboxes). */
export const panelClass =
  "z-popover min-w-[8rem] overflow-hidden rounded-lg border border-border bg-surface-raised " +
  "p-1 text-sm text-fg shadow-overlay";

/** Items inside menus and listboxes. */
export const itemClass =
  "relative flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 outline-none " +
  "data-[highlighted]:bg-surface-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50";
