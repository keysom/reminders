# Reminders

一个用 Go 写的日期提醒工具，适合配合 GitHub Actions 每天定时运行，并通过 PushPlus 推送到微信。

## 功能

- 支持一次性提醒：`repeat: once`
- 支持每年提醒：`repeat: yearly`
- 支持每月提醒：`repeat: monthly`
- 支持提前 N 天提醒：`remind_days: [7, 3, 1, 0]`
- 支持 GitHub Actions 定时运行
- 支持 PushPlus 微信推送

## 配置提醒事项

编辑 `reminders.yml`：

```yaml
timezone: Asia/Shanghai
remind_days: [7, 3, 1, 0]
always_push: true

reminders:
  - name: 房租
    date: "2026-08-01"
    repeat: monthly
    remind_days: [3, 1, 0]
    note: 提前准备转账，顺手检查水电费。

  - name: 妈妈生日
    date: "1975-08-12"
    repeat: yearly
    remind_days: [14, 7, 1, 0]
    note: 提前想礼物，当天发祝福。
```

`repeat` 可选：

- `once`：只提醒这一次
- `yearly`：每年同月同日提醒
- `monthly`：每月同日提醒

`remind_days` 表示距离事件还有多少天时提醒，`0` 表示当天。

`always_push: true` 表示即使当天没有匹配事项，也会推送一条“今天没有需要提醒的事项”。

## 本地运行

```powershell
go mod tidy
go run . -config reminders.yml -dry-run
```

也可以指定日期测试：

```powershell
go run . -config reminders.yml -today 2026-07-13 -dry-run
```

## GitHub Actions 推送到微信

1. 注册/登录 PushPlus，拿到 token。
2. 在仓库里打开 `Settings -> Secrets and variables -> Actions`。
3. 新建 secret：`PUSHPLUS_TOKEN`。
4. 进入 `Actions`，手动运行 `日期提醒`，或等待每天自动运行。

默认运行时间是北京时间每天 07:30，可在 `.github/workflows/reminder.yml` 里修改 cron。
