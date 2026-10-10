import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap, useRevisionedMutation, type Schema } from "@/shared/api";

export type AdminNotes = Schema<"AdminNotesState">;

/** A vendor (`app` omitted) or an application. */
export interface NotesOwner {
  vendor: string;
  app?: string;
}

export function notesKey(owner: NotesOwner) {
  return owner.app === undefined
    ? queryKey("getVendorNotes", { vendor: owner.vendor })
    : queryKey("getAppNotes", { vendor: owner.vendor, app: owner.app });
}

function read(owner: NotesOwner, signal: AbortSignal) {
  const { vendor, app } = owner;
  return app === undefined
    ? unwrap(
        api.GET("/admin/api/vendors/{vendor}/admin-notes", {
          params: { path: { vendor } },
          signal,
        }),
      )
    : unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/admin-notes", {
          params: { path: { vendor, app } },
          signal,
        }),
      );
}

/** Notes of `owner`; with `enabled: false` they load only on `refetch()`. */
export function useNotes(
  owner: MaybeRefOrGetter<NotesOwner>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  return useQuery({
    queryKey: computed(() => notesKey(toValue(owner))),
    queryFn: ({ signal }) => read(toValue(owner), signal),
    enabled: computed(() => toValue(enabled)),
  });
}

/** Replaces the notes; they have their own revision, independent of the entity. */
export function useNotesSave(
  owner: MaybeRefOrGetter<NotesOwner>,
  notes: MaybeRefOrGetter<AdminNotes | undefined>,
) {
  return useRevisionedMutation<AdminNotes, string>({
    revision: () => toValue(notes)?.revision,
    queryKey: () => notesKey(toValue(owner)),
    mutationFn: (text, ifMatch) => {
      const { vendor, app } = toValue(owner);
      const header = { "If-Match": ifMatch };
      return app === undefined
        ? unwrap(
            api.PUT("/admin/api/vendors/{vendor}/admin-notes", {
              params: { path: { vendor }, header },
              body: { text },
            }),
          )
        : unwrap(
            api.PUT("/admin/api/apps/{vendor}/{app}/admin-notes", {
              params: { path: { vendor, app }, header },
              body: { text },
            }),
          );
    },
  });
}
