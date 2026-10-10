import type { LocaleModule } from "@/shared/i18n";

export default {
  exchange: {
    modes: {
      label: "Template handling",
      linked: "Keep template link",
      linkedHint: "Built-in items keep following their template; only your changes are stored.",
      independent: "Independent copy",
      independentHint: "Writes the complete current settings without a template link.",
    },
    export: {
      action: "Export configuration",
      title: "Export {key}",
      description:
        "Download a ZIP package that can be imported into another RedApp. Files, caches, history and enabled state are never included.",
      includeApps: "Include all applications of this vendor",
      sensitive: "Sensitive content",
      includeNotes: "Include admin notes",
      includeCredentials: "Include proxy credentials",
      sensitiveWarning:
        "The package will contain private notes or proxy passwords. Store and share it securely.",
      scope:
        "Referenced categories and uploaded icons are included. Without credentials, proxy URLs that contain a password are left out.",
      submit: "Download ZIP",
      done: "Configuration exported.",
    },
    import: {
      action: "Import configuration",
      title: "Import configuration",
      description:
        "Upload a ZIP or YAML package, review what will change and decide per item. Nothing changes until you execute the import.",
      file: "Package file",
      fileHint:
        "New vendors and applications are created disabled. A preview expires after ten minutes.",
      uploading: "Uploading package",
      preview: "Preview import",
      updatePreview: "Update preview",
      execute: "Execute import",
      items: "{count} items",
      expires: "Preview expires",
      kinds: {
        vendor: "Vendor",
        app: "Application",
        category: "Category",
      },
      actions: {
        create: "Create",
        update: "Update",
        skip: "Skip",
        keep: "Keep",
      },
      actionLabel: "Action",
      actionFor: "Action for {key}",
      renamedTo: "will be imported as",
      targetVendor: "Target vendor",
      targetId: "Target application ID",
      dictionaryUpdate: "Update the names of the existing category",
      detach: "Detach the existing item from its template",
      updateNotes: "Replace the admin notes",
      templateChanged:
        "The built-in template differs from the exported one; this server's template is used.",
      templateMissing:
        "The template of this item does not exist on this server; it can only be imported as an independent copy.",
      proxy: {
        label: "Proxy",
        hint: "The package does not contain this proxy setting. Choose what to use.",
        keepCurrent: "Keep the current own setting",
        keepEffective: "Keep the currently effective proxy",
        inherit: "Inherit",
        direct: "Direct connection",
        url: "Proxy URL",
      },
      requirements: {
        confirm_detach_template: "Confirm detaching the template before importing.",
        resolve_omitted_proxy: "Choose a proxy setting.",
        resolve_changed_proxy: "Resolve the proxy change caused by the template change.",
        confirm_notes_update: "Confirm replacing the admin notes.",
      },
      differences: "{count} changed field | {count} changed fields",
      before: "Current",
      after: "Imported",
      changedTitle: "Decisions changed",
      changed: "Update the preview to apply your decisions before importing.",
      staleTitle: "Preview no longer valid",
      stale:
        "The preview expired or the data changed since it was made. Nothing was imported. Create a new preview.",
      notReady: "Resolve the items marked above, then update the preview.",
      trustWarning:
        "This package changes usage instructions. Instructions may contain HTML and JavaScript that run on the public application page; they are shown above as text only.",
      trust: "I trust the instructions in this package",
      doneTitle: "Import complete",
      done: "{count} item imported. | {count} items imported.",
      receipt: "Imported items",
      revision: "revision {revision}",
      disabledHint: "Newly created vendors and applications are disabled until you enable them.",
    },
    copy: {
      action: "Copy application",
      title: "Copy {key}",
      description:
        "Creates a new, disabled application with the same settings. Cache, files, history and tasks are not copied.",
      targetVendor: "Target vendor",
      targetId: "New application ID",
      idHint: "Lowercase letters, digits and single hyphens.",
      vendorInvalid: "Enter an existing vendor ID.",
      idInvalid: "Use lowercase letters, digits and single hyphens.",
      includeNotes: "Copy admin notes",
      notice: "An inherited proxy setting follows the target vendor.",
      submit: "Copy application",
      done: "Copied to {key}.",
      exists: "An application with this ID already exists.",
    },
  },
} satisfies LocaleModule;
