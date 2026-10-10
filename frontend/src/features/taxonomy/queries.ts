import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/vue-query";
import {
  api,
  fetchAllPages,
  queryKey,
  unwrap,
  useRevisionedMutation,
  type Schema,
} from "@/shared/api";

export type Category = Schema<"Category">;
export type CategoryPatch = Schema<"CategoryPatch">;

export const CATEGORY_PAGE_SIZE = 25;

/** Case-insensitive NFC comparison, as the server compares names and tags. */
export function foldText(value: string): string {
  return value.normalize("NFC").trim().toLocaleLowerCase();
}

/** Every category (all pages), for pickers. */
export function useAllCategories() {
  return useQuery({
    queryKey: queryKey("listCategories", { all: true }),
    queryFn: async ({ signal }) => {
      const { items } = await fetchAllPages((page) =>
        unwrap(
          api.GET("/admin/api/categories", { params: { query: { page, limit: 100 } }, signal }),
        ),
      );
      return items;
    },
  });
}

export function useCategoryList(params: MaybeRefOrGetter<{ q: string; page: number }>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listCategories", { ...toValue(params), limit: CATEGORY_PAGE_SIZE }),
    ),
    queryFn: ({ signal }) => {
      const { q, page } = toValue(params);
      return unwrap(
        api.GET("/admin/api/categories", {
          params: { query: { q: q || undefined, page, limit: CATEGORY_PAGE_SIZE } },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
  });
}

export function categoryKey(category: string) {
  return queryKey("getCategory", { category });
}

export function useCategory(
  category: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => categoryKey(toValue(category))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/categories/{category}", {
          params: { path: { category: toValue(category) } },
          signal,
        }),
      ),
    enabled: computed(() => toValue(enabled) && toValue(category) !== ""),
  });
}

/** Renames a category (or resets built-in names); 409 keeps the draft. */
export function useCategoryPatch(
  category: MaybeRefOrGetter<string>,
  current: MaybeRefOrGetter<Category | undefined>,
) {
  const client = useQueryClient();
  return useRevisionedMutation<Category, CategoryPatch>({
    revision: () => toValue(current)?.revision,
    queryKey: () => categoryKey(toValue(category)),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: queryKey("listCategories") });
    },
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PATCH("/admin/api/categories/{category}", {
          params: { path: { category: toValue(category) }, header: { "If-Match": ifMatch } },
          body,
        }),
      ),
  });
}
