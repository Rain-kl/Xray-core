# 2026-11-12 上游同步

用户授权将配套核心同步纳入面板合并。普通 merge 保留 `c662a740` 与上游 `c4561418` 双亲；上游 87 个提交，冲突仅发布脚本和生成 protobuf。发布脚本以上游新构建命令为基线，保留 feat 版本注入，Android 接入 packed relocations；protobuf 从保留 fork 字段的 proto 重新生成。

更新独立 REALITY 供应目录至上游 go.mod 对应版本，避免旧副本与新的 crypto/ecdh、crypto/mldsa 配置 API 不匹配；重贴允许纯 X25519 的最小握手补丁，并保留重复有效 key share 拒绝。模块内 replace 不会传递给调用方，面板验证须在 workspace 顶层显式引用本地 REALITY。发布和模块地址的处理留在面板依赖说明。

验证：原生限速、审计、mixed/SOCKS/HTTP、infra/conf 与 trafficlog 套件通过；common/log、common/protocol、dispatcher 与 mixed/SOCKS/HTTP 的 race 检查通过；原生二进制编译成功，vprotogen 的 check 模式通过。发布默认目标更新为 v26.11.9-feat.4，避免新基线重用旧 v26.10.10-feat.4 发布名。
