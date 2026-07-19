# 阿里云 ASK(Serverless K8s)部署自建 Fork 镜像教程

本文档记录在**阿里云 ASK 无服务器 Kubernetes 集群**上运行 New API,并把线上镜像从上游官方镜像切换为**自己 Fork 构建的定制镜像**的完整过程。

> 适用场景:你 Fork 了 New API 做了定制(例如新增前端功能),线上却跑着上游官方镜像 `calciumion/new-api:latest`,导致定制功能不生效。需要用自己构建的镜像替换线上镜像。

---

## 背景与问题定位

### 现象

线上定制功能(例如控制台侧边栏的「概览」模块)在页面上看不到,但代码明明已经合入自己的分支。

### 根因

线上 Deployment 跑的是**上游作者发布的官方镜像**,而非自己 Fork 构建的镜像。官方镜像里没有你的定制代码,自然看不到定制功能。

### 如何确认线上跑的是哪个镜像

用集群连接配置(`KubeConfig.yml`)连上后,查运行中 Pod 的**实际镜像 ID(带 digest)**,这是最可靠的证据:

```bash
export KUBECONFIG=/path/to/KubeConfig.yml

# 查 Deployment 声明的镜像
kubectl -n newapi get deploy new-api \
  -o jsonpath='{.spec.template.spec.containers[0].image}'

# 查 Pod 实际拉取运行的镜像(带 registry 域名 + digest,不可伪造)
kubectl -n newapi get pods -l app=new-api \
  -o custom-columns='NAME:.metadata.name,IMAGE:.status.containerStatuses[0].image,IMAGE_ID:.status.containerStatuses[0].imageID'
```

若 `IMAGE_ID` 形如 `docker.io/calciumion/new-api@sha256:...`,说明线上跑的是**上游 DockerHub 官方镜像**,不含你的定制。

---

## 集群环境说明(本次实例)

| 项目 | 值 |
| --- | --- |
| 集群类型 | 阿里云 ASK 无服务器 K8s(Pod 直接跑在 ECI 弹性容器实例上,无真实节点) |
| 地域 | 新加坡 `ap-southeast-1` |
| 节点 | virtual-kubelet 虚拟节点(非真实机器) |
| 业务命名空间 | `newapi` |
| Deployment | `new-api`,副本数 2 |
| 对外暴露 | `LoadBalancer` 类型 Service,挂阿里云公网 SLB |
| 数据库 / Redis | 外部托管服务,通过 Secret `newapi-secrets` 注入,集群内无状态 |

> ⚠️ ECI 是无服务器容器,**没有真实节点可以 `docker load` 本地镜像**,因此镜像**必须推到镜像仓库**再由集群拉取。

---

## 前置要求

| 工具 | 用途 | 检查命令 |
| --- | --- | --- |
| `kubectl` | 连接和操作集群 | `kubectl version --client` |
| `gh`(GitHub CLI) | 配置 secret、触发 CI 构建 | `gh auth status` |
| Docker(可选) | 本地构建(不推荐,见下文) | `docker version` |
| DockerHub 账号 | 存放自建镜像 | — |
| `KubeConfig.yml` | 阿里云集群连接配置 | 阿里云控制台下载 |

---

## 关键决策:为什么用 GitHub Actions 构建,而不是本地构建

本次踩过的坑,直接给结论:

- **集群 ECI 是 `amd64` 架构**,而开发机常见是 **Apple Silicon(arm64)Mac**。
- 本地用 `docker buildx --platform linux/amd64` 跨架构构建,会走 **QEMU 模拟**,编译两套前端 + Go 极慢(实测 30 分钟仍未完成),且 `bun install` 在模拟环境下会偶发 **`IntegrityCheckFailed`** 导致构建失败。
- **改用 GitHub Actions 的原生 amd64 runner** 构建,又快又稳(约 5~6 分钟),不占用本地机器,是生产环境推荐做法。

> 结论:**跨架构镜像构建优先用 CI 的原生 runner,不要在 arm Mac 上用 QEMU 硬扛。**

---

## 完整操作步骤

### 阶段 A:用 GitHub Actions 构建并推送自建镜像

#### A1. 新建 Fork 专用构建 workflow

不要直接改上游自带的 `docker-build.yml`(它硬编码推送到 `calciumion/new-api`,且会污染与上游的同步)。**新建一个专用 workflow**,推到你自己的 DockerHub 命名空间。

新建 `.github/workflows/fork-docker-build.yml`:

```yaml
name: Fork build & push (DockerHub youaijj)

on:
  workflow_dispatch:
    inputs:
      tag:
        description: 'Image tag to publish (e.g. 20260719-overview)'
        required: true
        type: string

jobs:
  build_and_push:
    name: Build & push (amd64)
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Check out
        uses: actions/checkout@v4
        with:
          fetch-depth: 1

      - name: Write VERSION
        run: |
          echo "${{ github.event.inputs.tag }}" > VERSION

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Build & push
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64
          push: true
          tags: |
            youaijj/new-api:${{ github.event.inputs.tag }}
            youaijj/new-api:latest-fork
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

> 把 `youaijj` 换成你自己的 DockerHub 用户名。`platforms: linux/amd64` 必须与集群架构一致。

#### A2. 配置仓库 Secret

CI 需要两个 secret 才能登录并推送 DockerHub:

```bash
# DockerHub 用户名
echo "youaijj" | gh secret set DOCKERHUB_USERNAME -R firstoneteam/new-api

# DockerHub Access Token(在 https://app.docker.com/settings/personal-access-tokens 生成,权限选 Read & Write)
echo 'dckr_pat_你的token' | gh secret set DOCKERHUB_TOKEN -R firstoneteam/new-api

# 确认
gh secret list -R firstoneteam/new-api
```

> ⚠️ **切勿把 token 明文贴到聊天 / 提交 / 日志里**。若不慎泄露,立即到 DockerHub 撤销并重新生成。

#### A3. 提交 workflow 并触发构建

workflow 文件必须先推送到仓库默认分支才能被触发:

```bash
git add .github/workflows/fork-docker-build.yml
git commit -m "ci: add fork docker build workflow publishing to youaijj/new-api"
git push origin main

# 触发构建,tag 建议用「日期-短sha-特性」便于追溯,不要用 latest
gh workflow run fork-docker-build.yml -R firstoneteam/new-api -f tag=20260719-499562b0-overview

# 查看运行状态
gh run list -R firstoneteam/new-api --workflow=fork-docker-build.yml --limit 3
```

#### A4. 验证镜像已推送

CI 成功后,从构建日志确认 digest 和目标仓库:

```bash
# 取最近一次 run 的 id 后
gh run view <run-id> -R firstoneteam/new-api --log \
  | grep -iE "pushing manifest|containerimage.digest"
```

看到 `pushing manifest for docker.io/***/new-api:<tag>@sha256:...` 即推送成功。

> 本地 `docker buildx imagetools inspect` 若超时,通常是本机到 DockerHub 的网络(IPv6)问题,不影响集群拉取。

---

### 阶段 B:更新集群 Deployment 指向新镜像

#### B1. 记录回滚点

改之前先记下当前镜像,便于回滚:

```bash
export KUBECONFIG=/path/to/KubeConfig.yml
kubectl -n newapi get deploy new-api \
  -o jsonpath='{.spec.template.spec.containers[0].image}'; echo
kubectl -n newapi get pods -l app=new-api
```

#### B2. 更新镜像触发滚动更新

```bash
kubectl -n newapi set image deployment/new-api \
  new-api=youaijj/new-api:20260719-499562b0-overview

kubectl -n newapi rollout status deployment/new-api --timeout=180s
```

> **ECI 首次拉镜像较慢**(实测约 1 分半),`rollout status` 可能先超时,属正常。滚动更新期间旧 Pod 继续服务,有 readiness 探针保护,不中断。

#### B3. 观察拉取和启动

若 rollout 迟迟不完成,看新 Pod 事件确认是在拉镜像还是出错:

```bash
kubectl -n newapi get pods -l app=new-api
kubectl -n newapi describe pods -l app=new-api | grep -A15 "Events:"
```

正常会看到:
```
Pulling image "youaijj/new-api:..."
Successfully pulled image "youaijj/new-api:..." in 1m33s
Started container new-api
```

> 若报 `ImagePullBackOff`,说明 DockerHub 仓库是**私有**的,需要给命名空间配 `imagePullSecret`(见附录)。DockerHub 免费账号新建仓库默认公开,一般无需此步。

#### B4. 验证上线成功

```bash
# 确认运行的是新镜像
kubectl -n newapi get pods -l app=new-api \
  -o custom-columns='NAME:.metadata.name,STATUS:.status.phase,IMAGE:.spec.containers[0].image'

# 经公网 SLB 健康检查
curl -s -o /dev/null -w "HTTP %{http_code}  time=%{time_total}s\n" \
  http://<SLB-IP>/api/status
```

看到全部 Pod 跑新镜像 + `HTTP 200` 即上线成功。最后到浏览器**强制刷新**(`Cmd+Shift+R`)清前端缓存,验证定制功能可见。

---

## 日后发版流程(固化)

改完代码后,只需三步:

```bash
# 1. 触发 CI 构建(换新 tag)
gh workflow run fork-docker-build.yml -R firstoneteam/new-api -f tag=<新版本号>

# 2. 等 CI 成功(gh run list 查看)

# 3. 更新集群镜像
export KUBECONFIG=/path/to/KubeConfig.yml
kubectl -n newapi set image deployment/new-api new-api=youaijj/new-api:<新版本号>
kubectl -n newapi rollout status deployment/new-api --timeout=180s
```

---

## 回滚

一条命令回滚到上一个版本:

```bash
export KUBECONFIG=/path/to/KubeConfig.yml
kubectl -n newapi rollout undo deployment/new-api

# 或回滚到指定历史版本
kubectl -n newapi rollout history deployment/new-api
kubectl -n newapi rollout undo deployment/new-api --to-revision=<N>
```

---

## 附录:私有镜像仓库配置 imagePullSecret

若 DockerHub 仓库设为私有,集群拉取需要凭证:

```bash
kubectl -n newapi create secret docker-registry dockerhub-cred \
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=youaijj \
  --docker-password='<DockerHub Access Token>'

# 给 Deployment 挂上 imagePullSecret
kubectl -n newapi patch deployment new-api -p \
  '{"spec":{"template":{"spec":{"imagePullSecrets":[{"name":"dockerhub-cred"}]}}}}'
```

---

## 经验总结(踩坑要点)

1. **诊断先看 Pod 的 `imageID`(digest)**,而非只看 Deployment 声明,能 100% 确认线上实际跑的镜像。
2. **跨架构构建用 CI 原生 runner**,别在 arm Mac 上用 QEMU 模拟 amd64,慢且易触发 `bun install` 完整性校验失败。
3. **镜像 tag 不要用 `latest`**,用「日期-短sha-特性」这类可追溯的 tag,方便定位和回滚。
4. **不改上游自带 workflow**,新建 Fork 专用 workflow 推到自己的命名空间,避免污染上游同步。
5. **ECI 无真实节点**,镜像必须走仓库,不能本地 load;首次拉镜像慢属正常。
6. **Secret / Token 严禁明文外泄**,泄露立即撤销重建。
7. **改线上前先记录回滚点**,滚动更新有 readiness 保护,配合 `rollout undo` 可快速止损。
