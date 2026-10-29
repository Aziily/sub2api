# Google Drive 接入研究

目标是在现有 Sub2API 管理界面中查看用户明确授权的 Drive 文件和容量，并逐步支持上传、下载与后台任务。Drive 凭据应与 Antigravity 凭据分开：现有授权范围不包含 Drive，不能直接假定它可以访问网盘。

## 建议的第一版

1. 在 Google Cloud 项目启用 Drive API，配置自己的 OAuth 客户端、回调和同意屏幕；为每个 Google 账号单独授权。
2. 新增独立的 Drive 连接记录，保存用户身份、授权范围和加密后的刷新令牌；不要把令牌返回给浏览器或写入日志。支持断开连接和撤销状态。
3. 先请求 `drive.file`，结合 Google Picker 只操作用户选择或应用创建的文件；调用 `about.get` 查询可返回的容量信息，区分总使用量与 Drive 使用量。实际权限及容量字段以授权后的测试为准。
4. 后端提供受现有管理员权限保护的连接状态、容量、文件列表、上传与下载接口。上传使用可恢复协议；对 403/429 做有限退避，显示明确错误。
5. 页面增加 Drive 标签：账号、授权状态、容量与最近同步；文件操作按账号隔离，审计日志不记录文件内容或凭据。

初期先验证单账号：授权、查看容量、上传/下载测试文件、刷新令牌、撤销和错误处理。以后再评估任务队列和多账号管理，不将多个账号容量描述成一个没有限制的存储池。

## 注意事项

- `drive.file` 的文件范围有限；完整列出或访问所有已有文件需要不同权限，并可能触发 Google 的验证要求。不要为了第一版默认申请全盘权限。
- 外部应用处于 Testing 时，包含 Drive 等范围的刷新令牌通常在七天后过期；生产状态和应用验证须按 Google 规定处理。
- `about.storageQuota` 的 `usage` 是跨服务总使用量，`usageInDrive` 是 Drive 文件使用量；部分账户可能没有 `limit`。
- 大文件传输、文件大小、API 配额及未来费用应以当前官方文档和实际 Cloud 项目配额为准。
- 如需现成实现，可对比 rclone 或 OpenList，但不能将其令牌不加区分地复用到现有模型账号。

参考：[Drive 权限范围](https://developers.google.com/workspace/drive/api/guides/api-specific-auth)、[about 资源](https://developers.google.com/workspace/drive/api/reference/rest/v3/about)、[OAuth 生命周期](https://developers.google.com/identity/protocols/oauth2)、[Drive API 配额](https://developers.google.com/workspace/drive/api/guides/limits)。

## 当前分支进度

`backend/internal/pkg/googledrive` 提供只读容量及分页文件列表客户端。调用方必须提供带有效 OAuth 凭据的 HTTP client；此包不获取、存储或暴露令牌，也未接入后台路由或界面。Google 的 [about.get 文档](https://developers.google.com/workspace/drive/api/reference/rest/v3/about/get)列出 `drive.file` 为受支持的授权范围，但容量字段仍需真实账号验证。

下一步是实现独立 OAuth 授权及回调（state、PKCE、回调地址校验）、加密凭据存储和按管理员授权的连接管理，再接入管理页面。功能需要自己的 Google Cloud OAuth 客户端，不能假定 Antigravity 客户端允许增加 Drive 范围。验证真实授权之前，不应暴露可用的文件操作入口。
