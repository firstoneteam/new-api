# Cloudflare 免费 CDN 接入教程（前端静态资源加速）

> 适用场景：源站部署在海外 VPS、出口带宽小，导致浏览器首屏加载前端 JS/CSS 极慢（如首屏 1~2 分钟）。通过 Cloudflare 免费版 CDN 将静态资源缓存到就近边缘节点，首屏可从数十秒降到数秒。
>
> **本方案零代码改动**：不改前端/后端代码、不重新打包、不重启服务、不改 nginx（可选加真实 IP 配置）。全程只在 Cloudflare 后台 + 域名注册商后台操作。

## 为什么用 Cloudflare 免费版

- **成本 ¥0**，无需 ICP 备案（Cloudflare 不接入国内备案体系）。
- 适合 `.ai`、`.org` 等**无法在工信部备案**、因而用不了国内 CDN（阿里云/腾讯云国内节点）的域名。
- 国内用户**无需翻墙**即可访问，只是被调度到就近的境外节点（香港/日本/新加坡等），仍远快于直连海外小水管源站。
- 自带免费 HTTPS、HTTP/3、Brotli 压缩、OCSP stapling。

## 前置须知（重要）

1. **免费版单请求 100 秒超时**：被代理的请求若 100 秒内没有任何响应字节，会返回 `524`。
   - 流式（SSE）模型接口：只要首字节在 100s 内吐出且持续有数据，就不受影响。
   - 非流式 + 超慢模型：若单次请求需等待 100s+ 才返回，会被掐断。这是免费版唯一需要留意的坑（大多数场景不会触发）。
2. **请求体上限 100MB**：上传超大文件（音频/大图）超过 100MB 会被拒。
3. 源站现有的 certbot 证书**继续保留、继续自动续期**，本方案不影响它。

---

## 准备工作

开始前先拿到两样信息：

1. **服务器公网 IP**：在服务器上执行 `curl -s ifconfig.me`（或 `curl -s ip.sb`），记下 IP。
2. **域名注册商**：确认 `token-router.ai` 是在哪里购买的（GoDaddy / Namecheap / 阿里云国际 / Cloudflare Registrar 等），第 3 步要登录那里改 nameserver。

> **域名就在 Cloudflare 购买（Cloudflare Registrar）？** 那么第 1、3 步大幅简化：
> - 域名的 nameserver 默认已指向 Cloudflare，注册时通常已自动创建对应站点（zone）——登录 Dashboard 一般能直接在站点列表看到 `token-router.ai`，**无需再 Add site，也无需改 nameserver（可跳过第 3 步）**。
> - 但要重点检查：现有 DNS 记录是否为 **🟠 橙色云朵（Proxied）**。若之前只用 Cloudflare 做 DNS 解析（灰色云朵），流量并未经过 CF 代理，**没有任何 CDN 加速效果**。把它切成橙色才是本教程的关键（见第 2 步）。
> - 后续第 4~8 步照常执行。

---

## 第 1 步：注册 Cloudflare 并添加站点

> 💡 **域名在 Cloudflare Registrar 购买的**：站点通常已自动创建，直接登录 Dashboard 在站点列表点开 `token-router.ai`，**跳过本步**，从第 2 步开始检查 DNS。

1. 打开 <https://dash.cloudflare.com/sign-up> 注册账号（邮箱 + 密码，免费）。
2. 登录后点 **Add a site / 添加站点**。
3. 输入主域名 `token-router.ai`（**不要带 `https://`，不要带 `www`**）。
4. 计划选 **Free（$0）**，继续。
5. Cloudflare 自动扫描现有 DNS 记录，稍等进入下一屏。

## 第 2 步：确认 DNS 记录 + 打开橙色云朵

1. 在扫描出的记录里找到主域名的 **A 记录**，确认其值 = 准备工作里记下的**服务器 IP**。
   - 若缺失，手动 **Add record**：类型 `A`，名称 `@`，IPv4 填服务器 IP。
2. 若有 `www` 记录一并保留。
3. 将这些指向 Web 服务的记录云朵图标设为 **🟠 橙色（Proxied）**——这是加速生效的关键。灰色云朵仅做 DNS 解析，不加速。
4. 点 **Continue / 继续**。

> ⚠️ 只把指向 Web 服务的记录设橙色。邮件相关的 MX、TXT 记录不要动。

## 第 3 步：去域名商修改 Nameserver（激活关键）

> ✅ **域名在 Cloudflare Registrar 购买的：本步可直接跳过。** 此类域名的 nameserver 天然指向 Cloudflare，站点默认即为 Active，无需修改。直接进入第 4 步。
>
> 以下仅适用于「域名在第三方注册商（GoDaddy/Namecheap/阿里云国际等）购买」的情况：

1. Cloudflare 会给出 **2 个 nameserver 地址**，形如：

   ```
   xxxx.ns.cloudflare.com
   yyyy.ns.cloudflare.com
   ```

2. 登录**购买域名的注册商后台**（不是服务器）。
3. 找到 `token-router.ai` 的 **Nameserver / DNS 服务器** 设置。
4. 删除原有 nameserver，替换为 Cloudflare 给的这两个。
5. 保存。

> 生效需几分钟~几小时（通常 1 小时内），期间网站照常访问不中断。生效后 Cloudflare 会**发邮件通知**，后台站点状态变为 **Active**。
>
> ⏸️ **务必等站点变为 Active 再做下一步。**

## 第 4 步：设置 SSL 模式（必须选对）

站点 Active 后，进 **SSL/TLS → Overview / 概述**：

1. 加密模式选 **Full (strict) / 完全（严格）** ✅
   - 源站有 certbot 有效证书，选此项最安全。
   - ❌ **切勿选 Flexible**，否则与 nginx 的 HTTPS 跳转形成重定向死循环，页面打不开。
2. 进 **SSL/TLS → Edge Certificates / 边缘证书**，打开：
   - **Always Use HTTPS**：On
   - **HTTP/3 (QUIC)**：On
   - **Minimum TLS Version**：1.2

## 第 5 步：开启 Brotli，关闭会破坏页面的优化

进 **Speed → Optimization**（新版界面在 **Rules** 下的各优化项，位置可能略有不同）：

1. **Brotli**：**On** ✅（免费压缩，务必开启）。
2. **Rocket Loader**：**Off** ❌（会打乱 JS 加载顺序，破坏 React 应用）。
3. **Auto Minify**（如可见）：JS 项**关闭**（前端包已压缩，避免二次处理出错）。

## 第 6 步：配置缓存规则（加速核心）

进 **Caching → Cache Rules**，点 **Create rule / 创建规则**，建立**两条**规则。

### 规则 A：静态资源强缓存

- **Rule name**：`cache-static`
- **匹配条件**：Field 选 `URI Path`，Operator 选 `starts with`，Value 填 `/static/`
- **动作**：
  - Cache eligibility → **Eligible for cache**
  - Edge TTL → **Override origin TTL** → `1 month`（或 `2592000` 秒）
  - Browser TTL → Override → `1 month`

> `/static/*` 是带内容 hash 的 immutable 资源，长缓存安全：内容变化时文件名的 hash 会变，不会读到旧文件。

### 规则 B：API 绕过缓存（保证接口不被缓存）

- **Rule name**：`bypass-api`
- **匹配条件**：点 **Edit expression / 编辑表达式**，粘贴：

  ```
  starts_with(http.request.uri.path, "/api/") or starts_with(http.request.uri.path, "/v1") or starts_with(http.request.uri.path, "/pg") or starts_with(http.request.uri.path, "/mj") or starts_with(http.request.uri.path, "/suno") or starts_with(http.request.uri.path, "/kling") or starts_with(http.request.uri.path, "/jimeng") or starts_with(http.request.uri.path, "/dashboard/billing")
  ```

- **动作**：Cache eligibility → **Bypass cache**

> ⚠️ GET 类接口（如 `/api/status`、`/dashboard/billing/usage`）若被缓存，用户可能看到他人数据，属正确性/安全红线，必须 Bypass。POST 请求 Cloudflare 默认不缓存。
>
> 前端页面路由（如 `/dashboard/overview`）默认不缓存、回源，由 nginx 的 `try_files ... /index.html` 处理 SPA 回退，无需额外规则。
>
> 建议把 `bypass-api` 规则排在 `cache-static` 之前，优先级更稳妥。

---

## 第 7 步：命令行验证

在**本地电脑**终端执行（无需登录服务器）：

```bash
# 1) 确认走了 Cloudflare + 静态资源被 Brotli 压缩
curl -sI https://token-router.ai/static/js/index.<hash>.js | grep -iE 'server|cf-cache-status|content-encoding'

# 2) 静态资源连打两次，第二次应命中缓存、耗时大幅下降
for i in 1 2; do
  curl -o /dev/null -s -w "run$i total=%{time_total}s\n" https://token-router.ai/static/js/index.<hash>.js
done

# 3) API 必须是 BYPASS/DYNAMIC，绝不能是 HIT
curl -sI https://token-router.ai/api/status | grep -i cf-cache-status

# 4) 首页可正常打开
curl -o /dev/null -s -w "home http=%{http_code}\n" https://token-router.ai/
```

> `index.<hash>.js` 需替换为实际文件名。可先 `curl -s https://token-router.ai/ | grep -oE '/static/js/[^"]+\.js'` 获取当前引用的 JS 文件名。

**预期结果：**

| 命令 | 预期 |
|------|------|
| ① | 出现 `server: cloudflare`、`content-encoding: br` |
| ② | 第二次 `total` 从数十秒降到 **1 秒内**（命中边缘缓存 `cf-cache-status: HIT`） |
| ③ | `cf-cache-status: BYPASS` 或 `DYNAMIC`（**不能是 HIT**） |
| ④ | `http=200` |

## 第 8 步：浏览器实测

1. 打开浏览器**无痕窗口**，访问 `https://token-router.ai`。
2. 首屏应从原来的 1~2 分钟降到**数秒**。
3. 登录并调用模型 API，确认功能正常，**尤其确认没有 524 超时报错**。

---

## 可选：让 nginx 记录真实用户 IP

接入 Cloudflare 后，源站看到的都是 Cloudflare 的 IP。若需在 nginx 日志/后端拿到真实用户 IP，在 nginx 的 `http` 或 `server` 块加入（Cloudflare 会把真实 IP 放在 `CF-Connecting-IP` 头）：

```nginx
# Cloudflare 官方 IP 段，完整列表见 https://www.cloudflare.com/ips/
set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
set_real_ip_from 103.22.200.0/22;
set_real_ip_from 103.31.4.0/22;
set_real_ip_from 141.101.64.0/18;
set_real_ip_from 108.162.192.0/18;
set_real_ip_from 190.93.240.0/20;
set_real_ip_from 188.114.96.0/20;
set_real_ip_from 197.234.240.0/22;
set_real_ip_from 198.41.128.0/17;
set_real_ip_from 162.158.0.0/15;
set_real_ip_from 104.16.0.0/13;
set_real_ip_from 104.24.0.0/14;
set_real_ip_from 172.64.0.0/13;
set_real_ip_from 131.0.72.0/22;
real_ip_header CF-Connecting-IP;
```

改完 `nginx -t && systemctl reload nginx`。此配置为可选，不影响加速效果。

---

## 常见问题

**Q：这会影响模型 API 调用吗？**
A：API 请求仍会穿过 Cloudflare 代理层，但按规则 B 不缓存。常规文本类流式接口基本不受影响；唯一风险是**非流式 + 单次超过 100s 的超慢请求**会返回 524。若出现，见下方进阶方案。

**Q：国内用户需要翻墙吗？**
A：不需要。Cloudflare 免费版未被封锁，国内用户正常访问，只是被调度到就近境外节点。对海外小水管源站而言仍是数量级提升。

**Q：需要改代码或重新打包吗？**
A：不需要。Cloudflare 直接缓存源站现有的 `/static/*` 文件。

**Q：certbot 证书还要维护吗？**
A：要，且照常自动续期。`Full (strict)` 模式依赖源站证书有效。

---

## 进阶（可选）：API 拆到灰色云朵子域名，彻底规避 100s 超时

仅当第 8 步实测出现 524 超时（存在超慢非流式模型）时才需要。思路是让 API 走独立子域名、**不经过 Cloudflare 代理**：

```
前端 + 静态资源：token-router.ai       → 🟠 橙色云朵（走 CF，加速）
模型 API 专用：  api.token-router.ai   → ⚪ 灰色云朵（仅 DNS，直连源站）
```

- 灰色云朵 = Cloudflare 只解析 DNS，流量直连源站，**无 100s 超时、无 body 限制、无 WAF 干预**，与当前直连一致。
- 需要：Cloudflare 加 `api` 子域名 A 记录（灰色）→ certbot 为 `api.token-router.ai` 签证书 + nginx 加对应 `server` 块 → **API 调用方把 base_url 改指向 `api.token-router.ai`**。
- 注意：灰色云朵不隐藏源站 IP；此方案仍**无需改 new-api 代码**，仅调整 DNS/nginx 与调用方配置。

---

## 与整体优化的配合

Cloudflare 解决的是**传输速度**（就近缓存 + Brotli）。若前端主包（`index.js`）本身偏大，可再配合**前端分包优化**（拆分 `splitChunks`、按需懒加载重型库如 katex/mermaid/highlight），两者叠加，首屏字节更少、命中更快。分包优化属纯前端构建配置调整，参见前端工程 `rsbuild.config.ts`。
