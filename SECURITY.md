# 安全策略

## 受支持的版本

仅 `main` 分支与最新 SemVer Release。旧 tag 不回溯修复。

主面板、从端与探针（[`santaizi-agent`](https://github.com/santaizi-group/santaizi-agent)）线协议不兼容旧版，须成对升级。

## 报告漏洞

请通过 [GitHub 私密安全公告](https://github.com/santaizi-group/santanzi-dashboard/security/advisories/new) 提交，**不要**用公开 Issue 或 PR 披露。

请附复现步骤、受影响版本与部署形态（Primary / Collector / 探针）。报告中请勿包含真实主机地址、Token 或私钥。

修复发布前请勿公开细节。

## 不在范围内

* 面板 Web 端口直接暴露公网导致的后果 —— 见 [README 安全要求](./README.md#安全要求必读)，这是部署选择，不是漏洞
* 上游 [哪吒监控](https://github.com/naiba/nezha) 未经本项目修改的代码 —— 请报给上游
* 第三方依赖自身漏洞 —— 请报给对应项目；如本项目未及时升级，可另行提 Issue

## gRPC TLS 与设备 mTLS

传输层 TLS 与设备证书分离：`node_uuid` 是业务身份，`SignedAgentCredential` 仍是探测授权。两套 CA 分钥：

* Agent CA → `urn:santaizi:agent:<uuid>` 客户端证（默认 `/var/lib/santaizi-dashboard/pki/agent-ca.{key,crt}`）
* Collector CA → `urn:santaizi:collector:<collector_uuid>` 客户端证（同目录 `collector-ca.{key,crt}`）

禁止用 Agent CA 给从端签身份证。禁止把任一 CA 私钥下发给探针或从端。Server 证/钥由运营提供，不用设备 CA 签。

默认 `grpc_tls.enabled=false`，`require_agent_mtls=false`，`require_collector_mtls=false`，旧 yaml 可启动。

```yaml
grpc_tls:
  enabled: false
  cert_file: /etc/santaizi/grpc-server.crt
  key_file: /etc/santaizi/grpc-server.key
  client_ca_file: ""
  require_agent_mtls: false
  require_collector_mtls: false
```

迁移顺序：

1. 升级主面板，两开关保持 false
2. 配置 Server 证书后打开 `grpc_tls.enabled`
3. 升级探针，让其 Enroll 拿到 Agent 证。卸载重装会换新 identity；旧探针已下线时，同一密钥允许换绑。仍在线的另一台探针继续拒绝。
4. 升级从端，Register+CSR 拿到 Collector 证与 Agent CA
5. 确认 Control / Ingest / Sync / Replicate 已走 mTLS
6. 再分别打开 `require_agent_mtls` 与 `require_collector_mtls`

`client_secret` 与从端 registration token 变为 bootstrap。`--insecure` / `primary_insecure_tls` 仅测试。
