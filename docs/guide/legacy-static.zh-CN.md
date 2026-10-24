# 与静态旧资源共存

[English](legacy-static.md)

保留原目录中的旧文件。存在的文件或目录由 Nginx 服务，完整路径不存在的请求交给 RedApp。只有精确 `/` 始终打开 RedApp。无需配置 Provider 或导入文件。

## Nginx 分流

以下 HTTP 示例是模板，不能替代现有 TLS、访问控制和代理配置。将 `downloads.example.invalid`、`/srv/legacy-root/`、`127.0.0.1:8080` 替换为实际域名、旧 root 和 RedApp 监听地址。目录中只能放允许公开的文件。现有隐藏文件、符号链接及私有目录限制必须同时覆盖原路径和 `/legacy/` 路径。

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

`root` 拼接请求路径，`alias` 替换匹配到的 `/legacy/` 前缀。alias 的 location 和文件系统目标都保留尾斜杠。整个旧目录树被映射，无需复制文件。原来的 `/tools/setup.zip` 继续有效；从 `/legacy/` 浏览会产生 `/legacy/tools/setup.zip` 这样的额外 URL。

目录请求由 Nginx 补尾斜杠。存在已配置的索引文件时，它优先于 autoindex。即使 `/tools/` 存在，缺失的 `/tools/missing.zip` 仍转 RedApp。文件存在但不可读或请求方法不受支持时，不会自动回退。不要把 RedApp 的 404 响应改成旧资源查询。

选择未使用的 alias 前缀。静态优先意味着已有 `/admin/`、`/assets/`、`/api/`、`/health/`、`/all` 或厂商/应用路径可能遮住 RedApp，包括其 JavaScript 和 CSS。这是有意保留的优先级，启用前必须检查冲突。已有精确、前缀或正则 location 可能覆盖本示例，需结合完整配置运行 `nginx -t`。Vue 内置页面跳转可能不发送服务端请求，应避免旧页面路径与 RedApp 页面重叠。

使用容器时，将旧 root 只读挂载给 Nginx；RedApp 不需要这个挂载。不要把 RedApp 数据目录作为静态 root。按[配置说明](configuration.zh-CN.md)设置 RedApp 公开地址和受信任代理地址，尤其在 Nginx 终止 TLS 时。

## 导航入口

在**后台 → 站点设置 → 自定义导航**中添加标题、地址 `/legacy/`，并选择是否新标签页打开，然后保存站点设置。可添加、编辑、删除链接，用现有拖动手柄或键盘调整顺序。最多 20 项，每项纯文本标题为 1–80 个字符，各语言共用同一标题。列表为空时保留原有内置导航。

地址必须为根相对路径或 HTTP(S) 绝对地址，最多 2048 个 UTF-8 字节。不允许凭据、协议相对地址、空白、控制字符或反斜杠，包括编码形式。链接使用整页跳转，因此 `/legacy/` 会请求 Nginx，不会被 Vue 路由截获。新标签页链接使用 `noopener noreferrer`。导航与站点设置共用 revision 和冲突保护。

不能只映射精确 `/legacy/` 首页：原生 autoindex 生成的相对链接会指向 `/legacy/` 下的路径，映射整个目录树才能保持这些链接有效。自定义索引页面里的根相对或绝对链接、样式、脚本、分页需要另行检查，这不是透明 URL 重写。如果前缀兼容困难，可以用独立旧资源站子域指向相同 root，但需另外配置 DNS/TLS。

## 切换流量前验证

检查 `/`、已有文件、带及不带尾斜杠的已有目录、已有目录中不存在的文件、RedApp API/assets/admin 路径、`/legacy/` 的子链接。确认断点下载和已有访问限制继续生效。在两种语言及窄屏下验证导航。这些步骤都不要求把旧资源导入 RedApp。
