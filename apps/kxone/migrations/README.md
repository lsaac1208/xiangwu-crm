# 数据库迁移

`migrations/` 用于保存版本化 schema 变更脚本。应用启动时会按文件名顺序执行未应用的 `*.sql` 文件，并将版本、文件名、checksum 记录到 `schema_migrations` 表。

生产环境默认关闭 GORM `AutoMigrate`，schema 变更应追加迁移脚本并在部署前备份数据库；开发环境默认先执行 `AutoMigrate` 兜底，再执行版本化迁移，方便早期迭代。

约定：

- 文件名使用递增版本号，例如 `000001_baseline.sql`、`000002_add_xxx.sql`
- 破坏性变更必须包含回滚说明或单独的 down 脚本
- 加唯一索引前必须先清理历史重复数据
- 线上执行记录需要补充到 `docs/改进日志.md`
- 已应用迁移文件不得修改；如需调整，新增下一版迁移

相关环境变量：

- `MIGRATIONS_DIR`：迁移目录，默认 `migrations`
- `ENABLE_AUTO_MIGRATE`：是否继续执行 GORM AutoMigrate；生产默认 `false`，开发默认 `true`
