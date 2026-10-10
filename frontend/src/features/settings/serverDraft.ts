import { watch } from "vue";
import { useDirtyGuard } from "@/shared/forms";

export interface ServerDraftOptions<State> {
  /** The cached server state; `undefined` until loaded. */
  state: () => State | undefined;
  /** Whether the user changed the draft since it was last adopted. */
  dirty: () => boolean;
  /** Replaces the draft and its baseline with a server state. */
  adopt: (state: State) => void;
  /** Called instead of `adopt` when the server state changes under an edited draft. */
  keep?: (state: State) => void;
}

/**
 * A settings draft that follows the server state until the user edits it:
 * a refetch replaces an unchanged draft but never an edited one. Installs the
 * leave guard. `reset()` adopts the given (or current) server state, for
 * saving, reloading after a conflict and discarding.
 */
export function useServerDraft<State>(options: ServerDraftOptions<State>) {
  useDirtyGuard(options.dirty);
  watch(
    options.state,
    (state) => {
      if (!state) return;
      if (options.dirty()) options.keep?.(state);
      else options.adopt(state);
    },
    { immediate: true },
  );
  return {
    reset(state: State | undefined = options.state()) {
      if (state) options.adopt(state);
    },
  };
}
