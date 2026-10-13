# 二开特性说明

## 会话级双向流量审计

- **价值与场景**：供 `Rain-kl/3x-ui` 按客户端、入站和目标地址精确汇总网站流量，而非按客户端累计用量估算。
- **功能描述**：复用上游已关闭的 [PR #6840](https://github.com/XTLS/Xray-core/pull/6840)（原始提交 `22ddfb1007c68019aa1dfdb7e81311c2c6096c25`）。开启 Xray access log 后，每个已路由连接在结束时输出 `uplink=<bytes> downlink=<bytes>`；被拒绝连接仍即时输出零字节。复用现有统计写入器与 splice 补偿，覆盖嗅探、异步管道、MUX 和部分异常关闭路径。
- **计量边界**：数值为 Xray 代理链路观测到的应用层字节，不含底层 TLS/QUIC 开销，也不代表目标服务器确认接收的字节。长连接在结束前不显示最终用量；中断传输可能只含已完成的计数。只有 IP 目标且嗅探不到域名时无法还原网站。上游关闭该 PR 的原因是维护者认为日志计数范围过大，不是测试失败；本 fork 保留它是为满足审计需求。
- **上游修改原因与维护要点**：审计需要连接级双向计数，而官方 Stats API 仅有客户端累计值。上游合并时重点检查 `app/dispatcher/`、`app/log/`、`common/buf/`、`common/log/`、`features/stats/`、`proxy/`、`transport/pipe/` 相关改动，尤其是链路结束时机、splice、MUX/XUDP、嗅探和访问日志关闭路径。不要把旧核心无字节字段的访问记录当作零字节。

## 上游同步与发布

1. `origin` 指向 `Rain-kl/Xray-core`，`upstream` 指向 `XTLS/Xray-core`。在独立分支 `git fetch upstream` 并合并 `upstream/main`，比较 PR #6840 与新增上游代码，解决冲突后更新本文件。
2. 运行 `go test ./app/dispatcher ./app/log ./common/log ./common/buf ./transport/pipe ./testing/trafficlog`、`go test -race ./app/dispatcher ./transport/pipe`，构建核心并用 TCP、UDP、MUX、Vision 连接核对日志。
3. 首个审计版 tag 为 `v26.10.10-audit.1`，指向本 fork 的审计实现。用本 fork 的 Release 发布核心，记录 tag、源 commit、资产校验和；在 3x-ui 的 Docker、安装脚本和发布工作流中同步修改下载地址与版本。先发布核心再发布面板，混合版本时面板应明确区分缺失字节与零字节。
