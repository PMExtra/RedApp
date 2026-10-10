/** Nested messages; leaves are vue-i18n message strings. */
export interface LocaleMessages {
  [key: string]: string | LocaleMessages;
}
/** A locale module: one or more top-level namespaces it owns. */
export type LocaleModule = Record<string, LocaleMessages>;
/** `import.meta.glob(pattern, { eager: true, import: "default" })` of locale modules. */
export type LocaleGlob = Record<string, LocaleModule>;

/**
 * Merges the locale modules of an entry. Each module owns its namespaces;
 * two modules defining the same namespace is a programming error.
 */
export function mergeLocaleModules(glob: LocaleGlob): LocaleMessages {
  const merged: LocaleMessages = {};
  const owners = new Map<string, string>();
  for (const [file, module] of Object.entries(glob)) {
    for (const [namespace, messages] of Object.entries(module)) {
      const owner = owners.get(namespace);
      if (owner) throw new Error(`i18n namespace "${namespace}" defined in ${owner} and ${file}`);
      owners.set(namespace, file);
      merged[namespace] = messages;
    }
  }
  return merged;
}

/** Flattens nested messages to dotted keys. */
export function flattenMessages(messages: LocaleMessages, prefix = ""): Map<string, string> {
  const flat = new Map<string, string>();
  for (const [key, value] of Object.entries(messages)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") flat.set(path, value);
    else for (const [nested, text] of flattenMessages(value, path)) flat.set(nested, text);
  }
  return flat;
}
