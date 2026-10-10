export { api, unwrap, configureApi, csrfHeaders, isAdminApi } from "./client";
export type { ApiHooks, UnwrappedData, paths, components, operations } from "./client";
export {
  ApiError,
  isApiError,
  isAbortError,
  apiErrorFromBody,
  networkError,
  type ErrorCode,
  type ClientErrorCode,
  type AnyErrorCode,
} from "./errors";
export { describeError, type ErrorDescription } from "./describeError";
export { ifMatch, ifMatchHeader, revisionFromEtag } from "./revision";
export { queryKey, createQueryClient, shouldRetry, type OperationId } from "./query";
export { useRevisionedMutation, type RevisionedMutationOptions } from "./useRevisionedMutation";
export { uploadWithProgress, type UploadOptions, type UploadProgress } from "./upload";
export { spaRoutes, spaAllowedQuery, spaDocuments, errorCatalog } from "./spec.gen";

import type { components } from "./schema.gen";
/** Shorthand for a spec schema: `Schema<"PublicApp">`. */
export type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];
