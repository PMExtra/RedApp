// The form stack (vee-validate + zod) has its own entry point so that pages
// without forms (the public site) never download it.
export {
  zodSchema,
  formError,
  displayFormError,
  utf8Length,
  codePointLength,
  useDirtyGuard,
  confirmDiscardDrafts,
  leaveDiscardingDrafts,
} from "./forms";
export { default as FormField } from "./FormField.vue";
export type { FormFieldBinding } from "@/shared/ui";
