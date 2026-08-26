# 重新載入 ShadowDNS

ShadowDNS 支援透過 SIGHUP 進行具 transaction 性質的設定 reload。成功的 reload 會在
不中斷 DNS 服務的情況下替換可 reload 的 server state；失敗的 reload 則保留目前的
active state。部分設定會建立 process-lifetime listener，因此仍需 restart 才能套用。

## 觸發 reload

使用套件隨附的 systemd unit 時，執行：

```bash
sudo systemctl reload shadowdns
```

Container 環境則向 ShadowDNS process 傳送 SIGHUP。例如：

```bash
docker kill --signal HUP shadowdns
```

Reload 前，請先確認目前沒有進行中的 DNS-01 challenge。成功的 reload 會清空記憶體內
Ephemeral TXT store 的所有 record；失敗的 reload 則會保留這些 record。

## SIGHUP 會更新什麼

每次 SIGHUP 都會再次呼叫 unified configuration loader。ShadowDNS 會讀取設定檔、展開
`shadowdns.yaml` 中的 environment expression，並在替換 active reloadable state 前驗證
完整的 candidate configuration。

可 reload 的 state 包含：

- zone data 與 alias mapping；
- GeoIP database；
- rate-limit 設定；
- query-log 設定；以及
- 既有 reload path 所推導的其他 server state。

所有可能失敗的載入與驗證工作都會在 state swap 前完成。若 environment expression
無法解析、展開後的資料無效，或其他 reload 步驟失敗，ShadowDNS 會繼續以先前的 state
提供服務，並遞增 reload failure metric。Diagnostic 只會指出安全的 environment
variable 名稱與 YAML path，不會印出 environment-derived value。

## SIGHUP 不會更新什麼

Ephemeral API 與 DNS-over-HTTPS（DoH）server 都是 **startup-scoped**。其 listener 與
configuration object 會在 ShadowDNS process 啟動時建立。雖然 SIGHUP 會透過 unified
loader 重新載入並驗證對應的 YAML section，但不會替換已在運行的 API 或 DoH server。

下列變更需執行完整的 process restart 才能套用：

- 啟用或停用 Ephemeral API 或 DoH；
- 修改任一服務的 listen address；
- 修改 Ephemeral API access rule 或 token；以及
- 修改 DoH 或 ACME 設定。

成功的 SIGHUP 仍可改變運行中 Ephemeral API 查核的 zone origin，因為這些檢查使用可
reload 的 zone state；同時也會清空 Ephemeral TXT store。這兩種行為都不表示 API
listener 或其 startup-scoped configuration 已被替換。

CLI flag 與 DNS listen-address 變更同樣屬於 process-scoped。修改後請 restart
ShadowDNS。

## Reload 時的 process environment

Environment expansion 讀取的是 **loader 執行當下，既有 ShadowDNS process 可見的
environment**。因此，只有 process supervisor 或其他機制確實修改了運行中 process
的 environment，SIGHUP 才能看到變更後的 variable。僅修改互動式 shell、磁碟上的
environment file 或 deployment manifest，本身不會改變已在運行的 process。

例如 alias target 使用 `${ALIAS_TARGET}` 時，reload 會將目前 process 可見的 value
套用到可 reload 的 alias state：

```yaml
aliases:
  example.com:
    members:
      - "${ALIAS_TARGET}"
```

若 `ALIAS_TARGET` 缺失或為空，reload 會 fail closed，並保留先前的 alias state。

## 以 environment variable 使用 Kubernetes Secret

Kubernetes 會在啟動 container 時注入 Secret-backed environment variable。更新 Secret
object **不會**更新既有 Pod process 的 environment。因此，更新後若只傳送 SIGHUP，
loader 仍會讀到舊的 process-visible value，而不是新的 Secret value。

更新透過 `env` 或 `envFrom` 使用的 Secret 後，請 restart Pods：

```bash
kubectl apply -f shadowdns-secret.yaml
kubectl rollout restart deployment/shadowdns
kubectl rollout status deployment/shadowdns
```

替換後的 Pods 會在啟動時取得更新後的 environment，並以同一份設定建立新的
startup-scoped listener。多 replica deployment 應使用 rolling rollout，讓健康的
replica 在更新期間持續提供服務。

!!! warning "SIGHUP 不是 Secret refresh 機制"
    當 Secret 以 environment variable 暴露給既有 Kubernetes Pod 時，更新 Secret
    後只執行 `systemctl reload`、`docker kill --signal HUP` 或直接傳送 SIGHUP 都不足以
    取得新值。請執行 Pod rollout restart。

以 volume 掛載的 Secret 具有不同的 Kubernetes file-update 行為，但 ShadowDNS 仍只會
讀取 configuration loader 所引用的來源。不要因 projected file 改變就推論 environment
也已刷新：若 YAML 使用 `${NAME}`，該值來自 process environment，必須建立新的 Pod
process 才能取得更新後的 Secret-backed value。

## 操作判斷表

| 變更 | SIGHUP | Restart／rollout |
|------|---------|-----------------|
| Zone 或 alias state | 成功 reload 後套用 | 通常不需要 |
| GeoIP、rate-limit 或 query-log state | 成功 reload 後套用 | 通常不需要 |
| 已在運行中 process 變更的 environment value | Unified loader 會重新讀取 | 視 environment 的修改方式而定 |
| 透過 `env`／`envFrom` 暴露的 Kubernetes Secret | 無法取得更新後的 Secret value | **必須執行 Pod rollout** |
| Ephemeral API 設定 | 會驗證，但運行中的 server 不變 | **必須 restart 才能套用** |
| DoH／ACME 設定 | 會驗證，但運行中的 server 不變 | **必須 restart 才能套用** |
| DNS 或 HTTP listen address，或 CLI flag | 運行中的 listener／flag 不變 | **必須 restart 才能套用** |

無論執行 reload 或 rollout，完成後都應驗證 DNS answer，並監控
`shadowdns_reload_total{result="failure"}` 與
`shadowdns_config_last_reload_success_timestamp_seconds`。完整 metric reference 請見
[以 Prometheus 與 Grafana 監控](monitoring.md)。
