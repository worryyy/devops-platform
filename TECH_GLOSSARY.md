# 技术名词与概念详解

> 这份文档配合 `INTERVIEW_QA.md` 使用。QA 文档解决"面试怎么答",这份文档解决"这个技术到底是什么、为什么需要它"。
>
> 所有解释都围绕你项目描述里真正出现的概念。日志/Loki/Grafana 相关只在**附录**里简短带过,因为你项目描述四段里压根没提,不是面试主线。
>
> 阅读建议:**先读"为什么存在"再看定义**,很多技术名词光背定义没用,理解它要解决的问题才是关键。每个概念末尾的"容易混淆的点"是面试官最爱追问的死角。

---

## 目录

- [第一章 变更影响分析与构建(CI 侧)](#第一章-变更影响分析与构建ci-侧)
  - [1.1 Monorepo 单仓多模块](#11-monorepo-单仓多模块)
  - [1.2 依赖闭包 与 go list -deps](#12-依赖闭包-与-go-list--deps)
  - [1.3 Go Module 缓存(GOMODCACHE)](#13-go-module-缓存gomodcache)
  - [1.4 Go 编译缓存(GOCACHE)](#14-go-编译缓存gocache)
  - [1.5 BuildKit 与 Registry 层缓存](#15-buildkit-与-registry-层缓存)
  - [1.6 镜像 Digest 与 Tag](#16-镜像-digest-与-tag)
  - [1.7 Trivy 镜像扫描](#17-trivy-镜像扫描)
  - [1.8 GitHub Webhook](#18-github-webhook)
- [第二章 渐进式发布(Argo Rollouts)](#第二章-渐进式发布argo-rollouts)
  - [2.1 三种发布策略:Rolling / Canary / BlueGreen](#21-三种发布策略rolling--canary--bluegreen)
  - [2.2 Argo Rollouts 与 Deployment 的区别](#22-argo-rollouts-与-deployment-的区别)
  - [2.3 ReplicaSet(RS)与 Stable RS](#23-replicasetrs与-stable-rs)
  - [2.4 流量切分的原理(Nginx Traffic Routing)](#24-流量切分的原理nginx-traffic-routing)
  - [2.5 AnalysisTemplate 与 AnalysisRun](#25-analysistemplate-与-analysisrun)
  - [2.6 SLI 与 SLO](#26-sli-与-slo)
  - [2.7 双门禁:绝对阈值 + Stable 相对退化](#27-双门禁绝对阈值--stable-相对退化)
  - [2.8 NaN Sentinel:Inconclusive vs Failed](#28-nan-sentinelinconclusive-vs-failed)
  - [2.9 服务目录与 Profile](#29-服务目录与-profile)
  - [2.10 Preview 探活](#210-preview-探活)
- [第三章 回退与 GitOps 一致性](#第三章-回退与-gitops-一致性)
  - [3.1 GitOps 与声明式](#31-gitops-与声明式)
  - [3.2 Argo CD 的 selfHeal](#32-argo-cd-的-selfheal)
  - [3.3 为什么回退必须改 Git(selfHeal 冲突)](#33-为什么回退必须改-gitselfheal-冲突)
  - [3.4 rollouts abort / undo / kubectl rollout undo](#34-rollouts-abort--undo--kubectl-rollout-undo)
  - [3.5 部分唯一索引(Partial Unique Index)](#35-部分唯一索引partial-unique-index)
- [第四章 告警治理(Prometheus + Alertmanager)](#第四章-告警治理prometheus--alertmanager)
  - [4.1 Prometheus 整体架构](#41-prometheus-整体架构)
  - [4.2 Recording Rule](#42-recording-rule)
  - [4.3 group_left join](#43-group_left-join)
  - [4.4 Alertmanager 的三大功能:路由 / 分组 / 抑制](#44-alertmanager-的三大功能路由--分组--抑制)
  - [4.5 inhibition 规则的 source / target / equal](#45-inhibition-规则的-source--target--equal)
  - [4.6 为什么 deploy_id 要作为 equal key](#46-为什么-deploy_id-要作为-equal-key)
  - [4.7 CrashLoopBackOff](#47-crashloopbackoff)
  - [4.8 for 持续时间(Grace Period)](#48-for-持续时间grace-period)
  - [4.9 契约测试(promtool / amtool)](#49-契约测试promtool--amtool)
  - [4.10 Downward API 与 label_replace](#410-downward-api-与-label_replace)
- [附录 A:日志相关(项目描述未提,了解即可)](#附录-a日志相关项目描述未提了解即可)
- [附录 B:一句话速记表](#附录-b一句话速记表)

---

# 第一章 变更影响分析与构建(CI 侧)

## 1.1 Monorepo 单仓多模块

**是什么:** 所有服务(或模块)的代码放在**一个 Git 仓库**里,而不是每个服务一个仓库(Multi-repo)。

**为什么存在:**
- 多个服务共享公共库(比如 `internal/auth`),Monorepo 里改一次公共库,所有引用它的服务一次提交都能看到;
- 跨服务重构方便,一个 PR 改多个服务;
- 版本一致性:所有服务在同一commit 上,不会出现"A 用了 auth v1,B 用了 auth v2"。

**它带来的问题(正是你项目要解决的):**
- **全量构建**:改了一个服务,传统 CI 会把所有服务都重新构建一遍,因为它们在同一个仓库。这就是"全量构建耗时"的根源。
- 你的项目用 `git diff + go list -deps` 识别"到底哪些服务真的受影响",只构建受影响的,就是为了解决 Monorepo 的这个副作用。

**容易混淆的点:** Monorepo ≠ 微服务。Monorepo 是**代码组织方式**,微服务是**部署方式**。你项目是"Monorepo 里的微服务":代码在一起,但部署成 13 个独立服务。

---

## 1.2 依赖闭包 与 `go list -deps`

**是什么:**
- **依赖闭包(Dependency Closure)**:从某个起点出发,**递归地**找出它依赖的所有东西的完整集合。A 依赖 B,B 依赖 C,那 A 的依赖闭包是 {B, C}(可能更多)。
- **`go list -deps`**:Go 工具链的命令,列出某个包的完整依赖闭包。

**为什么存在:**
直接依赖好找(看 import 语句),但**间接依赖**(依赖的依赖)靠人眼看不出来。你改了 `internal/auth`,哪个服务间接 import 了它?光看每个服务自己的 import 不够,因为可能是 `serviceA → pkgX → internal/auth` 这种链。

`go list -deps ./cmd/user` 会递归算出 `user` 这个服务最终链接进二进制的**所有包**,把变更文件和这个集合做交集,就能判断 user 是不是真的受影响。

**项目里怎么用:**
```
1. git diff --name-only before..after  → 拿到所有变更文件
2. 对每个候选服务:go list -deps ./cmd/<service> → 拿到依赖闭包
3. 变更文件 ∩ 依赖闭包 ≠ 空 → 这个服务受影响,进 build_matrix
```

**容易混淆的点:**
- `go list -deps` 给的是**编译期**依赖,不是运行期。运行期动态加载的东西它抓不到(但 Go 一般没有)。
- 这个工具(`ecampus-impact`)的源码在**应用仓库**,不在你的 GitOps 仓库。你仓库消费的是它的输出 `impact.json`。面试被问"代码在哪",老实说在应用仓库。

---

## 1.3 Go Module 缓存(GOMODCACHE)

**是什么:** Go 下载的第三方依赖(module)在本地的缓存目录,环境变量 `GOMODCACHE` 指向它。

**为什么存在:**
没有缓存的话,每次 `go build` 都要重新 `go mod download` 把所有依赖从网络拉一遍。Go 依赖动辄几百个,冷下载几十秒到几分钟。缓存后,只有 `go.mod` 里新增或升级的依赖才需要下载,其余命中缓存。

**项目里怎么用:**
- 挂载到共享 PVC 的 `/cache/go-mod`(Jenkinsfile 第 604-622 行)
- 跨 build 复用:这次构建下完的 module,下次构建(只要 go.mod 没大改)直接用

**容易混淆的点:** GOMODCACHE 缓存的是**依赖源码**(module zip),不是编译产物。编译产物是 GOCACHE(下一个)。

---

## 1.4 Go 编译缓存(GOCACHE)

**是什么:** Go 编译器把每个包的编译结果缓存在本地,环境变量 `GOCACHE` 指向它。

**为什么存在:**
Go 是编译型语言,每次 build 要把所有源文件编译成机器码。但如果你只改了 2 个包,其他几十个包没变,重新编译它们就是浪费。Go 编译器会**按包**缓存编译结果:只要这个包的源文件(加上它的依赖)没变,直接复用缓存,不重编。

**项目里怎么用:**
- 挂载到共享 PVC 的 `/cache/go-build`
- 效果:改 2 个包的提交,只有这 2 个包(加上依赖它们的)重编,其余几十个包命中缓存

**GOMODCACHE vs GOCACHE 的区别(高频追问):**
| | GOMODCACHE | GOCACHE |
|---|---|---|
| 缓存什么 | 第三方依赖的源码 | 自己代码的编译产物 |
| 命中条件 | go.mod 没变 | 源文件没变 |
| 省了什么 | 重复下载 | 重复编译 |
| 粒度 | module 级 | package 级(更细) |

---

## 1.5 BuildKit 与 Registry 层缓存

**这是你项目里最技术含量的缓存,也是面试最容易被深挖的。务必吃透。**

### BuildKit 是什么

**是什么:** Docker 官方的新一代镜像构建引擎,替代传统的 `docker build`。用 `buildctl` 客户端 + `buildkitd` 服务端。

**为什么不用 `docker build`:**
1. **不依赖 Docker daemon**:BuildKit 自己是独立进程,在 K8s Pod 里跑不需要 DinD(Docker in Docker),更安全;
2. **并发构建无关 stage**:Dockerfile 多 stage 时,能并行的 stage 同时跑;
3. **支持远程缓存**(关键):能把缓存推到镜像仓库,跨机器复用。`docker build` 的缓存是本地的。

### Dockerfile 的分层结构(理解 Registry 缓存的前提)

Dockerfile 每条指令(`FROM`、`RUN`、`COPY`)产生一个**层(layer)**。构建时:
- 从上到下执行,每层都看 inputs 有没有变;
- 某层的 inputs 没变 → **命中缓存**,直接复用旧层,不执行这条指令;
- 某层的 inputs 变了 → 这层失效,**它和它后面的所有层都重新执行**。

典型 Go 服务 Dockerfile:
```dockerfile
FROM golang AS builder
COPY go.mod go.sum ./          # 层1:依赖描述
RUN go mod download            # 层2:下载依赖(只在层1变了才重跑)
COPY . .                       # 层3:复制源码(每次提交都变)
RUN go build ./cmd/user        # 层4:编译(层3变了所以重跑)
FROM alpine
COPY --from=builder /app/user  # 层5
```

关键:**层3(源码)每次提交都变,但层1(go.mod)不是每次提交都变**。如果 go.mod 没变,层2(`go mod download`)就命中缓存,不重复下载依赖。这就是分层缓存的价值。

### Registry 层缓存

**是什么:** 把上面说的"层"缓存**推到镜像仓库(registry)**,下次构建(哪怕在另一台机器上)从 registry 拉缓存。

**项目里怎么用(Jenkinsfile 第 106-135 行):**
```sh
buildctl build \
  --import-cache "type=registry,ref=crpi-gfwwpdquc14b7w22-vpc.cn-shanghai.personal.cr.aliyuncs.com/pulseops/buildkit-cache/<service>:main-amd64" \
  --export-cache "type=registry,ref=...,mode=max,image-manifest=true,oci-mediatypes=true" \
  ...
```

逐个参数解释:
- `--import-cache type=registry,ref=...`:从 registry 的这个 ref 拉缓存;
- `--export-cache type=registry,ref=...`:构建完把新缓存推回 registry;
- `mode=max`:**导出所有中间层**(不止最终层)。`mode=min` 只导最终层,中间层没法复用;
- `image-manifest=true,oci-mediatypes=true`:用 OCI 标准格式,兼容性更好;
- 每个服务**独立 cache ref**(`buildkit-cache/comment:main-amd64`),避免服务互相挤占。

**为什么用 registry 不用本地 PVC:**
- 本地 PVC 缓存只在**一个 Jenkins agent** 上有效,换 agent 就没了;
- Registry 缓存**跨机器复用**,任何 agent 都能拉到同一份缓存;
- Registry 是内容寻址的(digest),缓存命中准确。

### 三层缓存总览(背熟这张表)

| 缓存层 | 缓存什么 | 媒介 | 命中条件 |
|---|---|---|---|
| GOMODCACHE | 第三方依赖源码 | 共享 PVC | go.mod 没变 |
| GOCACHE | 自己代码编译产物 | 共享 PVC | 源文件没变 |
| BuildKit Registry 层 | Dockerfile 每个层 | 镜像仓库(TCR) | 该层 inputs 没变 |

**容易混淆的点(高频追问):**
- "GOCACHE 和 BuildKit 不重复吗?" → **不重复,互补**。GOCACHE 管 Go 包级编译复用(几十个包里只重编 2 个),BuildKit 管 Dockerfile 层级复用(`go mod download` 那层不重跑)。两个一起才能把构建时间压下来。
- "为什么 `mode=max`?" → `min` 只缓存最终镜像,中间层(比如 builder stage)下次还得重跑。`max` 把所有中间层都缓存,这是跨构建复用的关键。

---

## 1.6 镜像 Digest 与 Tag

**这是"不可变发布"的根基,面试必问。**

### Tag 是什么

**是什么:** 给镜像起的人类可读名字,比如 `comment:v1.2`、`comment:dev`、`comment:latest`。

**问题:Tag 是可变的。** 同一个 tag `comment:dev`,上午推了一个镜像,下午又推一个,两次的 `:dev` 指向**完全不同的镜像内容**。这导致:
- 跨环境发布不可靠:dev 环境用的 `:dev` 和 staging 用的 `:dev` 可能是不同镜像;
- 回滚不可靠:回滚到 `:v1.2`,但 `:v1.2` 可能被覆盖过。

### Digest 是什么

**是什么:** 镜像内容的 **SHA256 哈希**,形如 `sha256:abc123def...`(64 位十六进制)。

**关键特性:Digest 是内容寻址的、不可变的。**
- 同一个 digest **永远**对应同一份镜像内容(只要内容一字不差);
- 镜像内容变了一个字节,digest 就完全不同(哈希雪崩);
- Digest 不能被覆盖:registry 里 `sha256:abc123` 永远是那个镜像,推不上去第二个同 digest 的。

### 用 Digest 引用镜像

```
comment@sha256:abc123def...   ← 用 digest 引用(不可变)
comment:dev                    ← 用 tag 引用(可变)
```

**项目里怎么用:**
1. BuildKit 构建完,从 metadata 抽出 digest;
2. yq 把 **digest**(不是 tag)写进 GitOps values:`.image.digest = sha256:...`;
3. Helm chart 渲染成 `comment@sha256:...`;
4. Argo CD 部署这个精确 digest;
5. `wait-for-release.sh` 轮询 Pod 的 image 字段,确认包含 `@sha256:...`。

**这就是"同一 Digest 跨环境发布":** dev 验证过的 `sha256:abc123`,prod 也部署同一个 digest,**保证是同一份镜像内容**,不会被 tag 覆盖问题坑到。

**容易混淆的点:**
- "那还要 tag 干嘛?" → Tag 给人看(`:v1.2.3` 方便理解版本),digest 给机器用(保证不可变)。实际部署用 digest,展示和追溯用 tag。你项目里两个都写(values 里 tag 和 digest 都有),但 chart 渲染**优先用 digest**。
- "digest 比 tag 慢吗?" → 拉取速度一样,registry 都是按层拉。

---

## 1.7 Trivy 镜像扫描

**是什么:** Aqua Security 开源的容器镜像漏洞扫描工具,扫镜像里的操作系统包和语言依赖,对照 CVE 数据库报漏洞。

**项目里怎么用(Jenkinsfile 第 144-157 行):**
```sh
trivy image --exit-code 1 --severity CRITICAL --no-progress "$IMAGE@$digest"
```
- `--exit-code 1`:发现漏洞就让 CI 失败;
- `--severity CRITICAL`:只看 CRITICAL 级别(HIGH 及以下不拦,原型阶段不想被低危拖死);
- **用 digest 扫不用 tag**:保证扫的就是刚推的那个镜像,不会因为 tag 可变扫错。

**扫描时机:** BuildKit 构建+推送之后立即扫,扫描通过才进 GitOps。带 CRITICAL 漏洞的镜像根本进不了发布流程。

**容易混淆的点:** Trivy 扫的是**镜像里的依赖漏洞**(比如 alpine 的 openssl 有 CVE),不是代码逻辑漏洞。代码漏洞得靠 SAST 工具(如 gosec)。

---

## 1.8 GitHub Webhook

**是什么:** GitHub 上的事件钩子。仓库发生特定事件(push、PR、tag)时,GitHub 主动往你配的 URL 发一个 HTTP POST,带上事件信息。

**为什么存在:** 不用轮询。没有 webhook 的话,CI 得每隔几秒问 GitHub"有新提交吗",浪费资源。Webhook 让"事件 → 响应"是实时的、被动的。

**项目里有两层 webhook(别混淆):**

**链路 A:源码 push → 触发 Jenkins**
- GitHub 仓库配置 webhook,指向 Jenkins;
- 开发 push 代码,GitHub 发 push event 给 Jenkins,带 `before` 和 `after` 两个 commit SHA;
- Jenkins 用这两个 SHA 做 git diff(见 1.2)。

**链路 B:GitOps PR 合并 → 触发 Argo CD**
- GitOps 仓库配置 webhook,指向 Argo CD;
- Jenkins 提的 PR 合并到 main,GitHub 发 push event 给 Argo CD;
- Argo CD 即时感知,不用等 3 分钟轮询;
- 配置在 `install-argocd.sh` 里:`configs.secret.githubSecret` 是校验 webhook 签名的密钥。

**容易混淆的点:** Webhook 只是"通知",不带认证。GitHub 发的请求用 HMAC 签名,接收方(Jenkins/Argo CD)用共享密钥验签,防止伪造。所以安装时要配 `githubSecret`。

---

# 第二章 渐进式发布(Argo Rollouts)

## 2.1 三种发布策略:Rolling / Canary / BlueGreen

**这是面试最高频八股,必须背成肌肉记忆。**

### Rolling Update(滚动更新)
**是什么:** K8s Deployment 的默认策略。逐步用新 Pod 替换旧 Pod:先起新 Pod,等新 Pod Ready,再杀旧 Pod,直到全部替换。

**特点:**
- Pod 数逐步替换,但**不区分流量比例**(流量跟着 Endpoints 自动分布);
- 省资源(不需要双倍 Pod);
- 回滚慢(要重新拉旧镜像起旧 Pod);
- 风险中等:一旦开始,默认会滚到底,中途出问题靠 readiness probe 拦。

### Canary(金丝雀)
**是什么:** 先把**小比例真实流量**导到新版本,观察没问题再逐步加大比例(1%→5%→20%→50%→100%)。

**特点:**
- **按流量百分比切分**,不是按 Pod 数;
- 风险最分散:问题只影响小比例用户;
- 需要流量路由能力(Nginx/Istio)和监控支撑;
- 每个阶段观察 SLI,超阈值就 abort。

**名字由来:** 矿工带金丝雀下矿,鸟先中毒人才知道危险。Canary 发布就是让"一小部分流量先趟雷"。

### BlueGreen(蓝绿)
**是什么:** 维护两套完整环境(蓝=旧,绿=新)。新版本在绿色环境部署完整副本,验证通过后**一键把 100% 流量切到绿色**。

**特点:**
- **全量切换**(一刀切),不是渐进;
- 回滚秒级:流量切回蓝色;
- 资源开销大:切换瞬间新旧并存,需要双倍资源;
- 切换前可以做完整验证(不像 Canary 只能小流量验证)。

### 三者对比表(背熟)

| 维度 | Rolling | Canary | BlueGreen |
|---|---|---|---|
| 流量 | 跟 Pod 走 | **按比例渐进** | **一键 100% 切** |
| 回滚 | 慢(重新拉镜像) | 收回流量 | **秒级**(切回去) |
| 资源 | 最省 | 增量(看比例) | **双倍** |
| 验证 | 边滚边看 | 小流量真实验证 | 切换前完整验证 |
| 适用 | 低风险日常 | 高流量核心 | 低流量/需完整验证 |

**一句话记忆:** Rolling 求"省",Canary 求"准",BlueGreen 求"快回滚"。

### 项目里的对应(为什么这么选)
- **高流量服务 → Canary**:样本足够做 SLI 统计,渐进放量风险最小;
- **低流量服务 → BlueGreen**:样本太少 Canary 会误判(见 2.7),用 BlueGreen + 人工兜底;
- **低风险配置 → Rolling**:没必要搞 Canary/BlueGreen 的复杂度。

**容易混淆的点:**
- "Canary 和灰度发布一样吗?" → 基本同义。狭义上,金丝雀是灰度的一种(按比例),灰度还包括按用户/地域等维度。
- "Rolling 和 Canary 都是逐步替换,区别?" → Rolling 是按 **Pod 数**逐步替换(流量跟着 Pod 走),Canary 是按**流量百分比**精确切分(能精确控制多少流量到新版本)。Canary 需要流量路由层支持。

---

## 2.2 Argo Rollouts 与 Deployment 的区别

**是什么:** Argo Rollouts 是个 K8s 控制器,引入了一个新资源类型 `Rollout`(CRD),用来替代原生的 `Deployment`。

**为什么需要它(原生 Deployment 的局限):**
Deployment 只支持 RollingUpdate 和 Recreate,**没有 Canary/BlueGreen 的流量切分能力**。你写 `maxSurge: 25%`,它只是按 Pod 数滚,不能精确控制"5% 流量到新版本"。

Argo Rollouts 补上了:
- **Canary 策略**:支持按 setWeight 精确切流量百分比,集成 trafficRouting(Nginx/Istio);
- **BlueGreen 策略**:维护 activeService + previewService,一键切流;
- **AnalysisTemplate**:每个阶段自动跑 Prometheus 查询做门禁,过了自动放下一步;
- **Stable RS 保留**:保留上一个稳定版本的 ReplicaSet,回滚秒级。

**Rollout 和 Deployment 的关系:**
- Rollout **替代** Deployment(你创建 Rollout 就不创建 Deployment);
- Pod 还是由 ReplicaSet 管理(Rollout 也用 RS,只是逻辑更复杂);
- 你项目里 `fast-rolling` profile 用 Deployment,其他三个 profile 用 Rollout。

**容易混淆的点:** Rollout 不是 Deployment 的"增强版",是**独立资源类型**。一个服务要么用 Deployment 要么用 Rollout,不能两个都用。

---

## 2.3 ReplicaSet(RS)与 Stable RS

**是什么:** ReplicaSet 是 K8s 里**真正管理 Pod 副本数**的资源。它保证任意时刻有指定数量的 Pod 在跑。Deployment/Rollout 背后都是创建 RS 来管 Pod。

**RS 和版本的关系:** 每次镜像版本变化,会创建一个**新的 RS**。旧的 RS 保留(但副本数缩到 0)。所以集群里可能有多个 RS,每个对应一个历史版本。

**Stable RS(Argo Rollouts 概念):**
- Argo Rollouts 在 `rollout.status.stableRS` 里记录"当前稳定版本对应的 RS 名字";
- Canary 失败时,`rollouts abort` 把流量切回 Stable RS 对应的 Pod;
- 这是回滚秒级的原因:Stable RS 还在(只是副本数 0),abort 时瞬间扩容它、缩容 Canary RS。

**项目里怎么用:**
回退的第一层(L1)就是从 `kubectl-argo-rollouts get rollout -o json` 取 `status.stableRS`,再从那个 RS 的 image 字段抠出 digest。这就是"Stable 版本"的来源。

**容易混淆的点:**
- "Stable RS 和当前线上版本一样吗?" → 正常情况下一样(发版成功后 Stable = 当前)。但**发布过程中**不一样:Canary 发到一半,Stable RS 是旧版本,Canary RS 是新版本,流量在两者间切分。
- "RS 和 Pod 什么关系?" → RS 管 Pod。RS 定义"我要 3 个带这些 label 的 Pod",控制器发现只有 2 个就再起 1 个。

---

## 2.4 流量切分的原理(Nginx Traffic Routing)

**是什么:** Canary 发布要"5% 流量到新版本",这需要**流量路由层**配合,光靠 K8s 自己做不到。

**项目里用 Nginx Ingress 的 Canary 注解:**

Argo Rollouts 配置:
```yaml
strategy:
  canary:
    trafficRouting:
      provider: nginx  # 用 Nginx 做流量切分
```

工作原理:
1. Argo Rollouts 创建两个 Service:`<service>-stable` 和 `<service>-canary`,分别指向旧/新 RS 的 Pod;
2. 创建两个 Ingress:主 Ingress 指向 stable Service,Canary Ingress 指向 canary Service,带注解 `nginx.ingress.kubernetes.io/canary-weight: 5`;
3. Nginx Ingress Controller 读到这个注解,按权重把 5% 请求导到 canary Service;
4. setWeight 改成 20 时,Rollouts 改注解为 `canary-weight: 20`,Nginx 立即生效。

**为什么需要 trafficRouting(不能光靠 Pod 比例):**
假设你有 10 个 Pod,起 1 个 Canary Pod。**如果不做流量路由,流量按 Pod 数均分,Canary 拿到 10% 流量**(随机分布)。这有几个问题:
- 比例不精确(1/10 不是精确的 1%);
- 无法做"先 1% 再 5%"的精细控制;
- 单个 Pod 故障会影响这个比例。

trafficRouting 让流量比例和 Pod 数解耦:你可以起 3 个 Canary Pod 但只给它 1% 流量,精确控制。

**容易混淆的点:**
- "Nginx Ingress 和 Nginx 是一回事吗?" → 不是。Nginx Ingress Controller 是跑在 K8s 里的、会读 Ingress 资源自动配置 Nginx 的控制器。你不用手动改 nginx.conf。
- "为什么不用 Istio?" → Istio(Service Mesh)能做更精细的流量切分(按 header、用户),但引入 mesh 复杂度高。原型阶段 Nginx 够用。

---

## 2.5 AnalysisTemplate 与 AnalysisRun

**是什么:** Argo Rollouts 的两个 CRD,做**自动化发布门禁**。

- **AnalysisTemplate**:模板,定义"在每个 Canary 阶段跑什么指标查询、什么条件算通过/失败";
- **AnalysisRun**:每次实际分析时创建的实例,记录这次分析的结果。

### AnalysisTemplate 结构(项目里 `analysis-template.yaml`)

```yaml
metrics:
- name: canary-error-rate
  interval: 1m           # 每 1 分钟查一次
  count: 10              # 查 10 次(配合 interval 决定一个 step 的总时长)
  successCondition: result[0] < 0.01   # 错误率 < 1% 算成功
  failureCondition: result[0] >= 0.05  # 错误率 >= 5% 算失败
  failureLimit: 2        # 连续失败 2 次终止发布
  provider:
    prometheus:
      query: "ecampus:http_error_ratio:rate5m{revision='{{args.latest-hash}}'}"
```

**字段解释:**
- `interval`:多久查一次指标;
- `count`:总共查多少次;
- `successCondition` / `failureCondition`:基于返回值的判断条件;
- `failureLimit`:允许失败几次(不是一次失败就 abort,容忍瞬时抖动);
- `consecutiveSuccessLimit`:需要连续成功几次才放下一步;
- `provider`:指标来源(Prometheus)。

### 三种结果(关键!)

每次分析返回三种状态之一:
- **Successful**:successCondition 满足 → 这一步通过,放下一步;
- **Failed**:failureCondition 满足 → 发布失败,**自动回滚**;
- **Inconclusive**:两个条件都不满足(典型是返回 NaN)→ **暂停**,等人决策,不自动回滚也不自动前进。

这第三种状态是设计的精髓,见 2.8。

**容易混淆的点:** AnalysisTemplate 是"规则定义",AnalysisRun 是"一次执行"。就像 Deployment(RS 模板)和 RS(一次实例)的关系。

---

## 2.6 SLI 与 SLO

**是什么:**
- **SLI(Service Level Indicator)**:服务等级**指标**。具体可量化的指标,比如"错误率"、"P95 延迟"、"成功率"。
- **SLO(Service Level Objective)**:服务等级**目标**。给 SLI 设的目标值,比如"P95 延迟 < 500ms"、"错误率 < 0.1%"。

关系:SLI 是测量值,SLO 是目标值。你监控 SLI,目标是达到 SLO。

**项目里的 SLI(服务目录定义):**
- **错误率**:`http_errors / http_requests`(rate5m)
- **P95 延迟**:请求耗时的第 95 百分位
- **关键操作成功率**(可选):按 route 正则过滤关键接口的成功率

**项目里的 SLO(每个服务在 catalog 里配):**
- critical 服务:错误率 < 1%,P95 ratio < 1.2
- standard 服务:错误率 < 2%,P95 ratio < 1.5

**P95 是什么:** 把所有请求按耗时排序,第 95 百分位的值。意思是"95% 的请求比这个快"。比平均值更能反映尾部体验(长尾请求用户感受最差)。

**为什么用 P95 不用平均值:** 平均值会被大量快请求稀释。100 个请求里 99 个 10ms、1 个 10s,平均 ~100ms 看着还行,但那个 10s 的用户体验极差。P95 抓尾部。

**容易混淆的点:** 还有 SLA(Service Level Agreement),那是**合同**(违反了要赔钱)。SLO 是内部目标,SLA 是对外承诺。项目里只涉及 SLI 和 SLO。

---

## 2.7 双门禁:绝对阈值 + Stable 相对退化

**这是你项目设计最精妙的地方,务必讲清楚。面试官深挖就指这里。**

### 问题背景
Canary 门禁要判断"新版本能不能继续放量"。直觉上,看新版本自己的指标就够了(错误率 < 5% 就放)。但这有两个坑:

**坑一:新版本自己指标 OK,但其实在退化。**
假设 Stable 错误率平时是 0.1%,Canary 错误率 1%。如果只看绝对阈值(< 5%),Canary 通过了。但 1% 比 Stable 的 0.1% **差了 10 倍**!这个新版本其实在退化,只是没退化到绝对阈值以下。

**坑二:新版本指标差,但其实是 Stable 基线坏了。**
假设 Stable 最近因为某个老 bug 错误率飙升到 4%。Canary 错误率 3%。如果只看绝对阈值(< 5%),Canary 通过。但如果用相对比较,Canary(3%)比 Stable(4%)还好——这时候判 Canary 失败是错的,因为基线本身就坏了,比较无意义。

### 双门禁设计

**门禁一:绝对阈值(Canary 自己)**
- Canary 错误率 < maxErrorRate(critical 是 1%)
- 防"新版本绝对值太差"

**门禁二:Stable 相对退化(Canary 比 Stable)**
- Canary 错误率增量 < maxErrorRateIncrease(critical 是 0.3%)
- 但**只在 Stable 自己健康时才启用这个比较**
- 如果 Stable 自己错误率就高(基线坏了),相对比较无意义,这个门禁返回 NaN → Inconclusive

### 为什么 Stable 异常时走 Inconclusive 而不是 Failed

因为:**基线不可信时,任何比较结论都不可信**。
- 判 Failed → 回滚一个可能没问题的版本(误杀);
- 判 Success → 放过一个可能有问题的版本(漏过);
- 判 Inconclusive → 暂停等人看,**最安全**。

这个设计的哲学:**测量工具不可信时,最安全的动作是不动。**

**面试怎么讲(浓缩版):**
> Canary 门禁我设计了双轨:一是新版本的绝对错误率不能超阈值,二是新版本相对 Stable 的退化幅度不能超阈值。但相对比较只在 Stable 自己健康时才有效——如果 Stable 基线已经坏了,比较结果无意义,这时门禁返回 NaN,触发 Inconclusive 让发布暂停等人决策,而不是强行判失败回滚。

---

## 2.8 NaN Sentinel:Inconclusive vs Failed

**这是上一节的延续,讲清楚"样本不足/查询失败/基线坏"时到底发生什么。**

### NaN 是什么
NaN = Not a Number,数学上的"非数值"。`0 / 0` 在 Prometheus 里返回 NaN(不是报错,是返回 NaN 这个值)。

### 项目里的技巧
AnalysisTemplate 每个 metric 的查询末尾加:
```
ecampus:http_error_ratio:rate5m{...}
or on() (vector(0) / vector(0))
```

`vector(0)/vector(0)` = NaN。这行 `or` 的作用:
- 正常情况下,`ecampus:http_error_ratio` 有值,`or` 取这个值;
- 当样本不足导致 `ecampus:http_error_ratio` 查不到值(空向量),`or` 兜底返回 NaN。

这就是 **NaN sentinel**:用 NaN 作为"数据不可用"的哨兵信号。

### NaN 触发什么
回到 AnalysisTemplate 的三个条件:
- `successCondition: result < 0.01` → NaN < 0.01?**不成立**(NaN 和任何数比较都是 false);
- `failureCondition: result >= 0.05` → NaN >= 0.05?**也不成立**;
- 两个条件都不满足 → **Inconclusive**(不是 Failed)。

### Inconclusive vs Failed 的区别(关键)

| 状态 | 含义 | Rollouts 的动作 |
|---|---|---|
| **Failed** | 确认新版本有问题 | **自动 abort + 回滚** |
| **Inconclusive** | 数据不可用,无法判断 | **暂停**(Paused),等人决策 |

**为什么这么设计:**
- 数据不足时,判 Failed 会**误杀**好版本(只是没数据,不是版本有问题);
- 判 Success 会**漏过**坏版本(没数据不代表没问题);
- Inconclusive 暂停,让人介入,既不误杀也不漏过。

配合 `inconclusiveTimeout`(critical 是 15m):暂停超过 15 分钟还没人处理,自动终止发布(总不能一直挂着)。

**三种触发 Inconclusive 的场景:**
1. **样本不足**:Canary 刚起,请求数 < minSamples,错误率查不到;
2. **指标查询失败**:Prometheus 挂了或查询语法错;
3. **Stable 基线异常**:Stable 自己错误率高,相对比较无意义(见 2.7)。

**容易混淆的点:**
- "Inconclusive 和 Error 一样吗?" → 不一样。Error 是"查询执行出错"(比如网络问题),Inconclusive 是"查询成功了但结果无法判断"。Argo Rollouts 里 Error 有单独的 `consecutiveErrorLimit`。
- "为什么不设成默认 Failed?" → 因为误杀好版本的代价也很高(开发好不容易发版被莫名回滚)。暂停让人判断更稳妥。

---

## 2.9 服务目录与 Profile

**是什么:** 一个 YAML 文件(`configs/service-catalog.yaml`),集中描述每个服务的发布元数据:用什么发布策略、阈值多少、属于谁。

**为什么存在:**
不同服务流量、重要性、风险都不同,不能一套配置走天下。服务目录让每个服务**静态配置**自己的发布策略,CI 和 chart 渲染时读这个文件。

**项目里的四种 Profile:**

| Profile | 策略 | 场景 | 关键参数 |
|---|---|---|---|
| critical-canary | Canary | 高流量核心 | 阈值严(errorRate 1%),步长细(1%→5%→20%→50%→100%) |
| standard-canary | Canary | 中等流量 | 阈值松(errorRate 2%),步长大(20%→50%→100%) |
| controlled-bluegreen | BlueGreen | 低流量 | manualPromotion=true,previewReplicaCount=2 |
| fast-rolling | Rolling | 低风险配置 | 无 Analysis,15m 超时 |

**catalog CLI:**
`platform-server catalog --catalog service-catalog.yaml --services user,comment --environment dev`
- 纯只读的渲染器;
- 把人维护的 YAML(含 profile 定义 + 环境覆盖)渲染成扁平化的 JSON(`delivery-catalog.json`);
- Jenkins 和 chart 渲染都消费这个 JSON。

**环境覆盖:**
dev 环境流量小,3000 样本永远达不到。所以 catalog 支持 `environmentOverrides`,dev 环境把 minSamples 降到 50,让 dev 也能通过门禁。

---

## 2.10 Preview 探活

**是什么:** BlueGreen 策略里,新版本(Preview/绿色)起好后,**在切流之前**主动对它发请求验证能不能正常工作。

**为什么需要它(对应项目描述的"低流量服务低样本误判"):**
低流量服务用 Canary,样本太少统计无意义(见 2.7)。但 BlueGreen 如果不做任何验证就切流,等于盲切。所以用 Preview 探活**主动验证**,而不是被动等统计:

- 新版本起在 Preview Service 后面,**不接真实流量**;
- 用 curl 主动打 Preview Service 的健康检查和关键接口;
- 探活通过 → 人工审批 → 切流;
- 探活失败 → 不切,避免故障进生产。

**项目里怎么用:**
BlueGreen 的 `prePromotionAnalysis` 用的是 **Job provider**(主动 curl),不是 Prometheus provider(被动统计):
```yaml
prePromotionAnalysis:
  templates:
  - templateName: <service>-analysis-bluegreen-preview-job
```
这个 AnalysisTemplate 里每个 metric 是一个 curl 命令,对应 catalog 里 `previewProbes` 配置的探针(健康检查、关键接口)。

**探活 vs 被动统计的区别:**
- **探活(主动)**:我主动发请求,看响应对不对。不依赖真实流量,适合低流量服务;
- **统计(被动)**:我观察真实流量的指标。需要足够样本,适合高流量服务。

**容易混淆的点:** 探活不是 K8s 的 liveness/readiness probe。那些是 kubelet 对单个 Pod 的健康检查;Preview 探活是 Argo Rollouts 在切流前对整个 Preview 环境的功能验证,粒度和目的都不同。

---

# 第三章 回退与 GitOps 一致性

## 3.1 GitOps 与声明式

**是什么:**
- **声明式(Declarative)**:你描述"我想要什么样的状态",而不是"执行什么操作"。比如"我要 3 个 nginx Pod",而不是"启动一个 Pod、再启动一个、再启动一个"。
- **GitOps**:把 Git 仓库作为系统期望状态的**唯一事实源(Source of Truth)**,集群状态自动收敛到 Git 描述的状态。

**为什么存在:**
传统运维是**命令式**的(SSH 上去 `kubectl apply`、`apt install`),问题:
- 没人知道当前状态是怎么来的(谁改了什么?什么时候?);
- 漂移(drift):有人手动改了集群,Git 里不知道;
- 回滚难:得回忆"上一版是什么样"。

GitOps 解决:
- **Git 是事实源**:集群该长什么样,全在 Git 里;
- **审计天然有**:每次变更都是 commit,谁改的、为什么、什么时候,git log 全有;
- **自动收敛**:集群状态偏离 Git 了,自动拉回来(selfHeal);
- **回滚 = git revert**:回到某个 commit,集群自动回到那个状态。

**项目里的体现:**
- Jenkins 不直接 `kubectl apply`,而是改 GitOps 仓库的 values 文件,提 PR;
- PR 合并后,Argo CD 检测到 Git 变化,自动 sync 到集群;
- 集群状态永远是 Git 的镜像。

**容易混淆的点:** GitOps 是理念,Argo CD 是实现这个理念的工具(另一个常见的是 Flux)。

---

## 3.2 Argo CD 的 selfHeal

**是什么:** Argo CD 的一个配置项(`syncPolicy.automated.selfHeal: true`)。开启后,**集群状态偏离 Git 时,自动重新 sync 拉回**。

**为什么需要:**
有人手动 `kubectl edit deployment` 改了集群(比如调了副本数),这时集群状态 ≠ Git。没有 selfHeal 的话,这个手动改动会一直存在,直到下次 sync 才被覆盖。有 selfHeal,Argo CD 立即发现漂移,自动把集群拉回 Git 的状态。

**项目里所有 Application 都开了 selfHeal。**

**selfHeal 和回退的冲突(下一节详讲):**
selfHeal 是把集群拉回 Git。但回退时我们用 `rollouts abort` 先把集群拉回 Stable,**这时 Git 里还是失败版本**。selfHeal 一触发,又把集群拉回失败版本!这就是为什么回退必须同时改 Git——见 3.3。

---

## 3.3 为什么回退必须改 Git(selfHeal 冲突)

**这是项目第三条的核心,也是最容易体现"想过 failure scenario"的点。**

### 问题场景
假设发版失败,需要回退:
1. 你跑 `kubectl-argo-rollouts abort`,流量切回 Stable RS;
2. 此时集群状态:流量在 Stable 版本;
3. 但 **GitOps 仓库的 values 文件还写着失败的 digest**;
4. Argo CD 的 selfHeal 检测到"集群 ≠ Git"(集群在 Stable,Git 在失败版本);
5. selfHeal 自动 sync,把集群又拉回失败版本!**回退白做了。**

### 解决方案
回退必须**两步走**:
1. **先切流量**(`rollouts abort`):集群回到 Stable;
2. **再改 Git**(CI 用 yq 把 values 的 digest 改回 Stable digest,提补偿 PR):Git 也回到 Stable。

这样集群和 Git 都是 Stable,selfHeal 不会捣乱。

### 不改 Git 的其他风险
就算没有 selfHeal,不改 Git 也有问题:
- 下次有人改这个服务,基于 Git 里的失败 digest 发,等于埋雷;
- Git 和集群不一致,审计断裂,"为什么集群是 v1 但 Git 是 v2?"

**这就是项目描述里说的"避免出现集群已经回退,Git 中却仍保留失败版本的情况"。**

**面试怎么讲:**
> GitOps 下回退的关键是 Git 和集群必须同步。如果只用 `rollouts abort` 切流量,Git 里还是失败版本,Argo CD 的 selfHeal 会把集群又拉回失败版本。所以我的回退流程是两步:先用 `rollouts abort` 把流量切回 Stable RS,马上由 CI 用 yq 把 values 的 digest 改回 Stable digest 并提交补偿 PR。PR 合并后 Git 和集群都是 Stable,这才是真正的闭环回退。

---

## 3.4 rollouts abort / undo / kubectl rollout undo

**三种回退命令,对应不同场景,别搞混:**

### `kubectl-argo-rollouts abort <rollout>`
- **用于**:Canary 发版中失败;
- **效果**:立刻把流量切回 Stable RS,Canary RS 缩容;
- **特点**:快,因为 Stable RS 还在(副本数 0 → 扩容);
- **不改 Git**(所以还要配第二步改 Git)。

### `kubectl-argo-rollouts undo <rollout>`
- **用于**:BlueGreen 切流后(post-promotion)失败;
- **效果**:回滚到上一个 RS,activeService 切回旧 RS;
- **和 abort 区别**:undo 是"回滚到上一个",abort 是"终止当前发布"。

### `kubectl rollout undo deployment/<name>`
- **用于**:fast-rolling profile(原生 Deployment,没有 Rollout 控制器);
- **效果**:回滚到上一个 Deployment revision;
- **为什么单独**:Deployment 没有 Argo Rollouts 管,得用 kubectl 原生命令。

**项目里(`rollback-release.sh` 的 abort-traffic)按情况选:**
```sh
if Deployment 或 fast-rolling:   kubectl rollout undo
elif UNDO_ROLLOUT=1 (bluegreen):  rollouts undo
else (canary):                    rollouts abort
```

**容易混淆的点:** `abort` 不删 Canary RS,只是把流量切走、副本缩到 0。RS 保留是为了审计和可能的再次放量。

---

## 3.5 部分唯一索引(Partial Unique Index)

**是什么:** PostgreSQL 的索引类型,**只对满足条件的行建唯一索引**。

**为什么需要(项目场景):**
`service_releases` 表记录每次发布。一个 service+environment 应该只有**一条 stable 记录**(当前稳定版本),但可以有**多条历史记录**(releasing/failed/compensating,留审计)。

如果用普通唯一索引 `unique(service, environment)`,一个服务只能有一条记录,历史都没法留。

部分唯一索引解法:
```sql
create unique index service_releases_stable_unique
  on service_releases (service, environment)
  where release_status = 'stable';   -- 只对 stable 行建唯一约束
```
- 满足 `status='stable'` 的行,(service, environment) 唯一;
- 其他状态的行不受约束,可以有多条历史。

**配合 UPSERT:**
```sql
insert into service_releases (...) values (..., 'stable', ...)
on conflict (service, environment) where release_status = 'stable'
do update set ...
```
发版成功时 upsert:有 stable 就更新,没有就插入,保证每个 service+env 永远只有一条 stable。

**容易混淆的点:** 部分唯一索引是 PostgreSQL 特性,MySQL 没有(MySQL 只有普通唯一索引)。面试被问"为什么不用 MySQL",可以说这个特性是选 PG 的原因之一。

---

# 第四章 告警治理(Prometheus + Alertmanager)

## 4.1 Prometheus 整体架构

**是什么:** 时序数据库 + 指标采集 + 告警引擎,云原生监控的事实标准。

**核心组件:**
- **Prometheus Server**:核心,pull 模式定时从目标抓指标,存时序数据库,评估告警规则;
- **Alertmanager**:接收 Prometheus 推来的告警,做分组/抑制/去重/路由;
- **Exporter**:被采集端暴露指标的组件(node-exporter、kube-state-metrics);
- **Pushgateway**(可选):临时任务推指标的中转。

**Pull 模式(关键):**
Prometheus **主动**去拉(pull)目标的指标,不是目标推(push)。每个目标暴露一个 `/metrics` 端点,Prometheus 定时去 HTTP GET 这个端点。

**为什么 pull 不 push:**
- Prometheus 知道目标在哪(通过服务发现),主动控制采集节奏;
- 目标挂了 Prometheus 能立即发现(拉不到);
- push 模式下,目标可能疯狂推送压垮监控服务器。

**项目里的采集目标:**
- kubelet / cadvisor(节点和容器指标);
- kube-state-metrics(K8s 对象状态,Pod/Deployment 的 label/annotation);
- ingress-nginx(流量指标);
- argo-rollouts(发布状态);
- 业务服务的 `/metrics`(HTTP 指标)。

---

## 4.2 Recording Rule

**是什么:** Prometheus 的功能,把一个**常用的、计算量大的查询**预先算好,存成一个新的时间序列(新指标)。

**为什么需要:**
假设你有 14 条告警都要算"按 revision 聚合的错误率"。如果每条告警的 expr 都写完整计算逻辑,每分钟评估 14 次,每次都重算一遍,很贵。

Recording rule 解法:预先算好存成 `ecampus:http_error_ratio:rate5m`,14 条告警直接查这个新指标,便宜。

**项目里的 recording rule(4 条核心):**
- `delivery_platform:pod_release_info`:Pod 级 release 信息;
- `delivery_platform:release_info`:按 revision 聚合;
- `ecampus:http_error_ratio:rate5m`:错误率;
- `ecampus:http_request_duration_seconds:p95:5m`:P95 延迟。

**命名约定(面试加分点):**
Recording rule 的名字用冒号分层:`<scope>:<metric>:<aggregation>:<window>`。比如 `ecampus:http_error_ratio:rate5m` = ecampus 域 / 错误率指标 / 5 分钟速率。这种命名让指标层次清晰。

**容易混淆的点:** Recording rule 不是告警规则(Alerting rule)。前者是"预算指标",后者是"基于指标触发告警"。两者都在 Prometheus 配置里,但用途不同。

---

## 4.3 group_left join

**是什么:** PromQL 里的一种 join 语法,把两个时间序列按标签关联,允许"多对一"的关系。

**为什么需要(项目场景):**
告警(比如错误率)算出来只有 `(service, revision)` 标签,但告警还需要 `deploy_id`、`git_sha`、`image_digest` 这些 release 身份信息(用于关联日志和定位版本)。这些信息在另一个指标(`release_info`)里。

需要把错误率指标 join release_info 指标,把 release 身份"借"过来。

**语法:**
```promql
ecampus:http_error_ratio:rate5m
  * on(service, revision) group_left(deploy_id, git_sha, image_digest)
delivery_platform:release_info
```

逐部分解释:
- `* on(service, revision)`:按 service 和 revision 标签做匹配;
- `group_left(deploy_id, ...)`:左边(错误率)可能有多条匹配右边(release_info)一条,**左边是"多",右边是"一"**;
- `group_left` 后的括号:从右边"借"过来这些标签(deploy_id、git_sha 等)。

**为什么是 group_left 不是 group_right:**
错误率指标可能有多个 revision(每次发版一个 revision),但 release_info 里一个 revision 对应一组 release 身份。所以错误率(多)join release_info(一),用 group_left。

**项目里为什么这么设计(对应"告警触发时再关联"):**
错误率指标本身不带 deploy_id(那是高基数,不能进 label),只在**告警触发时**才 join 进来。这样:
- 平时只存低基数的 `(service, revision)` 指标,series 数可控;
- 告警触发时才 join 高维身份信息,不影响存储。

**容易混淆的点:** PromQL 的 join 和 SQL 的 join 不完全一样。PromQL 是基于**标签匹配**的,不是基于表连接。`on(...)` 是匹配条件,`group_left/right` 指明多对一的方向。

---

## 4.4 Alertmanager 的三大功能:路由 / 分组 / 抑制

**是什么:** Alertmanager 是 Prometheus 的告警处理中心,接收 Prometheus 推来的告警,做三件事:

### 1. 路由(Routing)
根据告警的标签,决定发到哪个 receiver(通知渠道)。
```yaml
route:
  receiver: default-receiver       # 默认
  routes:
  - matchers: [signal_type="deploy_context"]
    receiver: deploy-context       # deploy_context 类走单独 receiver
```

### 2. 分组(Grouping)
把相似的告警**合并成一条通知**,避免告警风暴。
```yaml
group_by: [alertname, namespace, service, deploy_id]
group_wait: 30s        # 等 30s,把这段时间的同类告警合并
group_interval: 5m     # 同一组下一封通知至少间隔 5 分钟
```
比如同一个 deploy_id 的多个告警,合成一条通知,而不是一条一条发。

### 3. 抑制(Inhibition)
当某个告警触发时,**自动抑制**相关的其他告警。(详见 4.5)

**项目里的状态(诚实边界):**
⚠️ Alertmanager 的两个 receiver(`default-receiver`、`deploy-context`)都是**空配置**,没接 webhook/邮件/钉钉。原型阶段只验证路由和抑制逻辑,通知渠道是预留。被问"告警发到哪",老实说没接通知渠道。

---

## 4.5 inhibition 规则的 source / target / equal

**这是告警治理的核心机制,必须讲清楚。**

**是什么:** Inhibition(抑制)规则定义:"当 A 告警触发时,抑制 B 告警"。用于避免相关告警重复打扰。

**规则结构:**
```yaml
- source_matchers:        # 当 source(源头)告警触发时
    - alertname="ReleaseDeployNoiseWindow"
    - deploy_id=~".+"
  target_matchers:        # 抑制 target(目标)告警
    - alertname="ReleasePodRestarting"
    - deploy_id=~".+"
  equal: [namespace, service, environment, deploy_id]   # 匹配条件
```

三个关键字段:
- **source**:触发抑制的告警(发布窗口打开);
- **target**:被抑制的告警(Pod 重启);
- **equal**:source 和 target 必须在这些标签上**值相等**,抑制才生效。

**equal 的作用(精确匹配同一次发布):**
发布期间集群里可能有多个版本(多个 deploy_id)。如果只说"发布窗口打开就抑制所有 Pod 重启",会误伤别的发布的告警。`equal: [deploy_id]` 保证只抑制**同一个 deploy_id** 的告警。

**项目里的两条抑制规则:**

**规则一:发布窗口抑制瞬时重启**
- source:ReleaseDeployNoiseWindow(发布窗口打开)
- target:ReleasePodRestarting(瞬时重启)
- 逻辑:发版期间 Pod 重启是正常的(滚动更新),抑制掉

**规则二:user_impact 抑制 noise(优先级保护)**
- source:user_impact 类告警(真影响用户了)
- target:ReleasePodRestarting
- 逻辑:既然已经告了真问题,就别再发重启噪声了

**关键:target 只能是 ReleasePodRestarting**
CrashLooping、StuckTerminating、ReplicaShortage、PodNotReady **永远不在 target 里**。这些是持续故障,必须透传,绝不能被抑制。

**容易混淆的点:**
- "抑制和静默(silence)一样吗?" → 不一样。Silence 是**手动**设置的静默窗口(比如维护期间),inhibition 是**自动**的、基于规则的抑制。
- "source 和 target 谁抑制谁?" → source 触发时,target 被抑制。记不住就记"source 是大哥,target 是被按住的"。

---

## 4.6 为什么 deploy_id 要作为 equal key

**为什么这么强调 deploy_id:**
发布期间,集群里同时存在多个版本:
- Stable 版本(deploy_id=A)在跑;
- Canary 版本(deploy_id=B)也在跑(小流量)。

假设 Canary(deploy_id=B)触发了 ReleaseDeployNoiseWindow,Stable(deploy_id=A)的 Pod 也在重启(不相关)。如果抑制规则不限定 deploy_id:
- Canary 的 NoiseWindow 会把 Stable 的 Pod 重启也抑制了 → **误伤**。

加上 `equal: [deploy_id]`:
- 只抑制 deploy_id=B(Canary)的 Pod 重启;
- deploy_id=A(Stable)的不受影响。

**deploy_id 的格式:** `<releaseBatch>-<service>-1`,比如 `ecampus-pipeline-main-10-comment-1`。每次发布唯一,精确到"这次发布的这个服务"。

---

## 4.7 CrashLoopBackOff

**是什么:** K8s 的 Pod 状态,表示**容器反复崩溃、反复重启**。kubelet 按 exponential backoff(指数退避)重试,状态显示 CrashLoopBackOff。

**常见原因:**
1. 应用启动崩溃(代码 bug、配置错误);
2. OOMKilled(内存不足被杀,重启又不够,循环);
3. liveness probe 配太激进(应用还没起来就被判不健康杀掉);
4. 依赖没准备好(数据库连不上启动失败);

**怎么排查(面试标准答案):**
```sh
kubectl describe pod <name>        # 看 Events,退出码
kubectl logs <pod> --previous      # 看上一次崩溃的日志(关键!不加 --previous 看的是当前这次)
```
退出码:137 = OOMKilled,1 = 应用错误,255 = 配置问题。

**项目里的对应:**
`ReleasePodCrashLooping` 告警 = `increase(kube_pod_container_status_restarts_total[10m]) >= 3`(10 分钟内重启 3 次以上)。这正是 CrashLoop 的信号。关键是**这个告警永远不被抑制**——CrashLoop 是真故障,不是发布噪声。

**CrashLoop 和 PodRestarting 的区别(核心):**
- **PodRestarting**:重启了 1 次(`for` 无 grace,立即告)。可能是发布正常重启(滚动更新会重启);
- **CrashLooping**:10 分钟重启 3+ 次(`for: 2m`)。是病,反复崩;
- 抑制规则只压 PodRestarting,永远不压 CrashLooping。

---

## 4.8 for 持续时间(Grace Period)

**是什么:** 告警规则里的 `for` 字段,表示"条件满足后,持续多久才真正触发告警"。

**为什么需要:**
指标会瞬时抖动。比如错误率某一秒飙到 100%(就 1 个请求还错了),下一秒恢复。如果不加 `for`,这种瞬时抖动也会告警,造成噪声。

`for: 5m` 表示"条件必须连续满足 5 分钟,才触发告警",过滤掉瞬时抖动。

**项目里的分级 for(对应"分级持续时间"):**

| 告警 | for | 为什么 |
|---|---|---|
| ReleaseDeployNoiseWindow | 无 | 上下文信号,要立即可用做抑制源 |
| ReleasePodRestarting | 无 | 瞬态信号,立即告(但可被抑制) |
| ReleasePodCrashLooping | 2m | 持续故障,但 2 分钟够判断不是偶发 |
| ReleasePodStuckTerminating | 2m | 同上 |
| ReleaseReplicaShortage | 5m | 副本不足可能正在扩容,给 5 分钟 |
| ReleasePodNotReady | 5m | 未就绪可能还在启动,给 5 分钟 |

**设计逻辑:**
- 瞬态信号(PodRestarting):立即告,但可抑制;
- 持续故障(CrashLoop):短 grace(2m),确认不是偶发就告;
- 可能自愈的(ReplicaShortage):长 grace(5m),给系统自愈时间。

**容易混淆的点:** `for` 是"持续满足",不是"延迟告警"。条件中途不满足了(比如错误率降下来),告警不会触发,pending 状态取消。

---

## 4.9 契约测试(promtool / amtool)

**是什么:** 对配置文件做**自动化校验**,防止人为误改导致行为偏差。

**promtool:** Prometheus 官方工具。
- `promtool check rules`:语法检查;
- `promtool test rules <test.yml>`:**单元测试**。喂入构造的时序数据,断言某告警在某时刻应该/不应该触发。

**amtool:** Alertmanager 官方工具。
- `amtool check-config`:Alertmanager 配置语法检查;
- `amtool config routes test`:给定一组 label,验证它路由到哪个 receiver。

**项目里的契约测试(`test-observability.sh`):**
不只检查语法,还做**语义断言**:
- inhibit 的 target 只能是 ReleasePodRestarting,不能出现 CrashLooping 等;
- inhibit 的 equal 必须包含 deploy_id;
- 各 alert 的 for 时长必须符合设计(NoiseWindow 无 for、CrashLooping 2m 等);
- amtool 路由测试:验证 deploy_context 类告警路由到 deploy-context receiver。

**为什么需要契约测试:**
告警规则是 YAML,容易改错。比如有人"优化"抑制规则,把 CrashLooping 加进 target,代码不会报错,但语义错了(CrashLoop 被抑制 = 真故障被吞)。契约测试就是防这种语义错误。

**诚实边界(必须知道):**
⚠️ 这套契约测试**代码完整,但没接进 GitHub Actions**。原因是 CI runner 没装 docker(这些工具用 docker 跑)。被问"CI 里跑了吗",老实说"本地跑通,还没接进流水线,是待办"。

---

## 4.10 Downward API 与 label_replace

**这是 release 身份怎么从 CI 流到告警的底层机制。**

### Downward API
**是什么:** K8s 的功能,把 Pod 的**元数据**(label、annotation、环境)以**环境变量**或**文件**形式注入容器。

**为什么需要:**
应用想知道自己的 deploy_id / git_sha,但这些是 Pod label,应用代码访问不到 K8s API。Downward API 把这些 label 映射成环境变量,应用直接读 env 就行。

**项目里(`_helpers.tpl`):**
```yaml
env:
- name: DEPLOY_ID
  valueFrom:
    fieldRef:
      fieldPath: metadata.labels['delivery.platform/deploy-id']
- name: GIT_SHA
  valueFrom:
    fieldRef:
      fieldPath: metadata.labels['delivery.platform/git-sha']
```
应用读 `DEPLOY_ID` 环境变量,输出到日志里,Alloy 采集。

### label_replace
**是什么:** PromQL 函数,给时间序列**改/加/删标签**。

**为什么需要:**
kube-state-metrics 暴露的 Pod label 名字是 `delivery_platform_deploy_id`(K8s label 用 `/` 和 `.`,Prometheus 标签只允许 `_`)。名字又长又不规范。用 label_replace 重命名成干净的 `deploy_id`:

```promql
label_replace(
  kube_pod_labels,
  "deploy_id",                   # 新标签名
  "$1",                          # 新标签值(从正则捕获)
  "label_delivery_platform_deploy_id",  # 旧标签名
  "(.*)"                         # 正则
)
```

**项目里的 recording rule(`delivery-platform-release-recording`):**
用 6 层 label_replace,把 `kube_pod_labels` 里又长又丑的 label 名,重命名成干净的 `deploy_id`、`git_sha`、`image_digest` 等,供后续告警 join。

**完整链路(面试可讲):**
```
CI 生成 deploy_id
→ Downward API 注入 Pod label
→ kube-state-metrics 抓 Pod label
→ label_replace 重命名成干净标签(recording rule)
→ 告警 join 这个 recording rule,带上 deploy_id
→ 告警 annotation 里渲染 loki_query,关联到日志
```

---

# 附录 A:日志相关(项目描述未提,了解即可)

> ⚠️ **你项目描述四段里没提日志,面试官从描述出发不会追问日志。** 这部分作为背景知识了解,不用深背。如果被问到,简单说一句"日志我用了 Loki 方案,但项目描述里没展开"即可。

### Loki 是什么
Grafana 出的日志聚合系统,定位是"日志版的 Prometheus"。

### Loki vs ELK
- **ELK(Elasticsearch)**:对日志正文建全文索引,查询快但资源贵(内存、磁盘);
- **Loki**:**只对标签建索引**,日志正文压缩成 chunk 存。便宜很多,但不擅长全文搜索。

### 为什么选 Loki
和 Prometheus 标签体系一致,指标和日志用同一套标签,关联方便。

### 高基数问题(如果被问)
Loki 只索引标签,如果把 `deploy_id`、`trace_id` 这种每次都不同的字段当标签,series 数爆炸,索引膨胀。解法:**高基数字段走 structured metadata(不建索引),只把低基数字段(namespace/service)当标签。**

### Promtail / Alloy
日志采集 agent。Promtail 是老的(每个节点一个 DaemonSet),Alloy 是新的(可以集群级 Deployment)。项目用 Alloy。

---

# 附录 B:一句话速记表

面试前快速过一遍:

| 术语 | 一句话 |
|---|---|
| Monorepo | 所有服务代码在一个仓库,共享方便但全量构建慢 |
| 依赖闭包 | 递归找全所有间接依赖,`go list -deps` 实现 |
| GOMODCACHE | 缓存第三方依赖源码,go.mod 没变就命中 |
| GOCACHE | 缓存编译产物,源文件没变就命中(包级) |
| BuildKit Registry 缓存 | 把 Dockerfile 每层缓存推到 registry,跨机器复用 |
| Digest | 镜像内容 SHA256,不可变,跨环境发布用它 |
| Tag | 镜像名,可变,给人看的 |
| Rolling | 逐步替换 Pod,最省资源 |
| Canary | 按流量比例渐进放量,风险最分散 |
| BlueGreen | 两套环境一键切流,回滚秒级但费资源 |
| Argo Rollouts | 替代 Deployment,支持 Canary/BlueGreen/Analysis |
| ReplicaSet | 真正管 Pod 副本数的资源,每个版本一个 |
| Stable RS | 上一个稳定版本的 RS,回滚时切回它 |
| AnalysisTemplate | 定义每个 Canary 阶段跑什么查询做门禁 |
| SLI | 指标(错误率、P95) |
| SLO | 目标(错误率 < 1%) |
| 双门禁 | 绝对阈值 + Stable 相对退化,防误判 |
| NaN sentinel | 数据不可用时返回 NaN,触发 Inconclusive 而非 Failed |
| Inconclusive | 数据不可用,暂停等人决策,不自动回滚 |
| Failed | 确认有问题,自动回滚 |
| Preview 探活 | BlueGreen 切流前主动 curl 验证(主动),区别于被动统计 |
| GitOps | Git 是唯一事实源,集群自动收敛 |
| selfHeal | 集群漂移时自动拉回 Git |
| 部分唯一索引 | 只对满足条件的行建唯一约束(PG 特性) |
| Recording rule | 预算常用查询存成新指标,省计算 |
| group_left join | PromQL 多对一关联,把 A 标签借给 B |
| inhibition | source 告警触发时抑制 target 告警 |
| equal | source 和 target 必须在这些标签上值相等 |
| deploy_id | 每次发布唯一标识,作为 equal key 精确匹配 |
| CrashLoopBackOff | 容器反复崩溃,真故障,永不被抑制 |
| PodRestarting | 重启一次,可能是发布正常,可被抑制 |
| for | 条件持续多久才告警,过滤瞬时抖动 |
| 契约测试 | 对配置做语义断言,防误改(promtool/amtool) |
| Downward API | 把 Pod label 注入容器环境变量 |
| label_replace | PromQL 改标签名,把丑名重命名成干净名 |
