# Coexisting with a static archive

[中文](legacy-static.zh-CN.md)

Keep the old files in their existing directory. Nginx serves each existing file or directory; requests whose complete path does not exist go to RedApp. Only the exact `/` always opens RedApp. No Provider or file import is needed.

## Nginx routing

This HTTP example is a template, not a replacement for your TLS, access-control or proxy configuration. Replace `downloads.example.invalid`, `/srv/legacy-root/` and `127.0.0.1:8080` with your domain, old root and RedApp listener. The directory must contain only files intended for publication. Keep existing restrictions on hidden files, symlinks and private directories in force for both the original and `/legacy/` paths.

```nginx
server {
    listen 80;
    server_name downloads.example.invalid;
    root /srv/legacy-root/;
    autoindex on;

    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;

    location = / {
        proxy_pass http://127.0.0.1:8080;
    }

    location = /legacy {
        return 301 /legacy/;
    }

    location /legacy/ {
        alias /srv/legacy-root/;
        autoindex on;
    }

    location / {
        try_files $uri $uri/ @redapp;
    }

    location @redapp {
        proxy_pass http://127.0.0.1:8080;
    }
}
```

`root` appends the request path; `alias` replaces the matched `/legacy/` prefix. Keep the trailing slash on both the alias location and its filesystem target. The alias exposes the whole old tree without copying files. Original URLs such as `/tools/setup.zip` remain valid; browsing `/legacy/` produces additional URLs such as `/legacy/tools/setup.zip`.

Nginx adds a trailing slash for directory requests. An existing configured index file takes precedence over autoindex. A missing `/tools/missing.zip` falls through to RedApp even when `/tools/` exists. Existing but unreadable files or unsupported request methods do not automatically fall through. Do not turn RedApp 404 responses into archive lookups.

Choose an unused alias prefix. With static-first routing, an existing `/admin/`, `/assets/`, `/api/`, `/health/`, `/all` or vendor/application path can shadow RedApp, including its JavaScript and CSS. This policy is intentional: check conflicts before enabling it. Existing exact, prefix or regular-expression locations may override this example; review the combined configuration with `nginx -t`. Vue's built-in page navigation may not make a server request; avoid overlapping old and RedApp page paths.

Mount the old root read-only in the Nginx container, if using containers; RedApp does not need that mount. Do not share RedApp's data directory as a static root. Configure RedApp's public URL and trusted proxy addresses as described in [configuration](configuration.md), particularly when TLS terminates at Nginx.

## Navigation entry

In **Administration → Site settings → Custom navigation**, add a title, URL `/legacy/`, and choose whether to open a new tab. Save the site settings. Add, edit, remove or reorder links using the existing drag handles or keyboard controls. Up to 20 entries are supported, each with a plain-text title of 1–80 characters. Titles are shared across languages. An empty list leaves the built-in navigation unchanged.

URLs must be root-relative paths or absolute HTTP(S) URLs, up to 2048 UTF-8 bytes. Credentials, scheme-relative URLs, whitespace, controls and backslashes (including encoded forms) are rejected. Links use full-page navigation, so `/legacy/` reaches Nginx rather than the Vue router. New-tab links use `noopener noreferrer`. Navigation changes share the site settings revision and conflict protection.

An exact `/legacy/` mapping alone is insufficient: native autoindex emits relative links that resolve below `/legacy/`. Mapping the whole tree keeps those links working. Custom index pages with root-relative or absolute links, styles, scripts or pagination need separate checking; this is not transparent URL rewriting. A separate archive subdomain serving the same root is an alternative if prefix compatibility is difficult; it needs its own DNS/TLS setup.

## Verify before switching traffic

Check `/`, an existing file, an existing directory with and without its trailing slash, a missing file inside an existing directory, RedApp API/assets/admin paths and `/legacy/` child links. Confirm download range requests and your existing access restrictions still behave correctly. Test navigation in both languages and on a narrow screen. None of these steps requires importing the archive into RedApp.
