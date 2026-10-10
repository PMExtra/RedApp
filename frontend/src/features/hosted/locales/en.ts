import type { LocaleModule } from "@/shared/i18n";

export default {
  hosted: {
    add: {
      title: "Add a file",
      replaceTitle: "Replace a file",
      description:
        "Files stay available until you delete or replace them. An import URL is downloaded once and not checked again.",
      source: "Source",
      upload: "Upload a file",
      import: "Import from a URL",
      file: "File",
      fileRequired: "Choose a file.",
      url: "HTTP(S) URL",
      urlHint:
        "Without credentials. Uses this application's outbound proxy; up to 4 redirects are followed.",
      urlInvalid: "Enter an http:// or https:// URL.",
      path: "Path",
      pathHint: "Where the file is published below this application, for example tools/setup.exe.",
      pathRequired: "Enter a path.",
      pathTooLong: "The path may contain at most 4,096 bytes.",
      replacing: "Replacing {path}. If it changed meanwhile, the replacement is rejected.",
      cancelReplace: "Cancel replacement",
      conflictTitle: "The file changed",
      conflict:
        "A file already exists at this path, or the file you are replacing was changed or deleted. Check the list below and try again.",
      save: "Save file",
      replace: "Replace file",
      clear: "Clear",
      saved: "{path} saved.",
    },
    progress: {
      label: "Transfer progress",
      of: "{done} of {total}",
      bytes: "{done} transferred",
      committing: "Saving the file…",
      cancel: "Cancel transfer",
      cancelled: "Transfer cancelled. Nothing was saved.",
    },
    list: {
      title: "Hosted files",
      description: "Published below this application's address.",
      empty: "No files yet.",
      path: "Path",
      size: "Size",
      sha256: "SHA-256",
      created: "Saved",
      actions: "Actions",
      replace: "Replace {path}",
      delete: "Delete {path}",
    },
    delete: {
      title: "Delete this file?",
      description:
        "{path} can no longer be downloaded unless you add it again. Running downloads finish first.",
      done: "{path} deleted.",
      gone: "{path} was already deleted or replaced.",
    },
  },
} satisfies LocaleModule;
