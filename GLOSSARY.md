# CRM 术语

基于当前 `apps/kxone/pkg/types`、README 和业务入口整理（2026-10-14）。字段和业务语义变化时同步更新。

| 术语 | 本项目含义 |
| --- | --- |
| 线索（Lead） | 招生或销售跟进对象，保存客户资料、来源、负责人及结果；主要类型为 `types.Lead`。 |
| 客户资料 | 姓名、电话、微信、地区、产品等资料。`ProfileVersion` 用于资料并发控制，与流程 `Version` 不同。 |
| 分配 | 将待分配线索派给负责人员；分配完成不表示接收人已领取。 |
| 领取 | 被分配人员确认接手线索，是独立于分配的操作。 |
| 转派 | 将线索交给另一负责人，需要检查角色、归属及历史；不是重新创建线索。 |
| 跟进 | 联系及结果记录，含备注、意向或星级和下次安排；主要类型为 `LeadFollowup`。 |
| 业务阶段（BusinessStage） | `invitation` 邀约、`negotiation` 谈单、`finished` 完结。 |
| 归属状态（OwnershipState） | 待分配、待领取、私有、公海或预约相关归属状态；与阶段、结果状态分别表达不同事实。 |
| 结果状态（LeadResultStatus） | `active`、`converted`、`invalid`、`closed`，表示业务有效、已转化、无效或关闭。 |
| 历史流程状态（LeadStatus） | 兼容 `pending_dispatch`、`assigned_pending_claim`、`claimed`、`following` 等状态的字段；不替代上述所有维度。 |
| 公海（PublicPool） | 可重新分配或领取的线索范围，访问和操作规则需查具体用例。 |
| 预约（LeadAppointment） | 来访或谈单安排，有独立负责人、时间及状态，不等同于线索本身状态。 |
| 邀约师 / 谈单师 | `inviter` / `closer`，分别负责邀约和谈单；`sales` 是仍兼容的销售角色。权限以当前代码为准。 |
| 管理员 / 主管 / 调度员 | `admin` / `manager` / `dispatcher`，表达不同角色，不互相代指。 |
| CRM Web | 位于 `apps/kxone/web` 的 CRM 前端。 |
| 验收通过 | 满足当前任务明确的用户行为标准且有实际证据；不单指 build 或局部测试通过。 |
