## Summary

把 backup zone 載入時對「apex SOA」「apex NS」這兩類記錄被 `filterBackupRecords` drop 所發出的 WARN 降為 DEBUG，剩下不在豁免名單內的類型仍然 WARN。

## Motivation

現行 spec（zone-parser「Backup-Override Zone Classification」）規定 backup zone 中非 TXT/MX/SRV 的記錄要 drop 並印 warning。可是另一條既有 spec 契約（alias-resolver + 已 archive 的 prune-backup-cli）要求 backup zone file 必須留著 SOA + apex NS 以維持 RFC 1035 合法性 — 換句話說，**這兩種類型每次載入「必然」會被 drop 一次**，因為 file 必留、runtime 必丟。

ns2 production 規模實測：剛 prune --apply 完並重啟 shadowdns，journal 立刻噴 37,493 條 WARN，全部都是「apex NS（29,991 條）」+「apex SOA（7,502 條）」這兩類，沒有其他類型。每條都是 spec 雙重契約決定下的常態事件，零 actionable signal，把真正該關注的訊號（parse error、unexpected type、operator 還沒清掉的 A/CNAME 殘留）完全淹沒。

operator 的觀感：「prune --apply 不是清掉了嗎為什麼還這麼多 WARN？」需要逐筆看 type 才知道全是無害的。這是純 UX 問題，但成本顯著。

## Proposed Solution

於 `internal/zone/classify.go` 的 `filterBackupRecords`：

1. 對被 drop 的 RR，依下列規則決定 log level：
   - `type == SOA` → **DEBUG**（zone 必有 SOA，必被 drop，零訊號）。
   - `type == NS` AND `canonical(owner) == canonical(zone origin)` → **DEBUG**（apex NS 是 zone 自我聲明，必有、必被 drop）。
   - 其他類型 → 維持 **WARN**（A/AAAA/CNAME/PTR/sub-delegation NS/不符合 overridable 規則的 TXT 等，這些是 operator 該行動的訊號，通常透過 `shadowdns prune-backup` 清理）。
2. 在每個 backup zone 載入結束時，**若該 zone 有 WARN-level drop（即 `other_dropped > 0`）**才印一條 **INFO** 摘要，內容含該 zone 的 SOA drop 數、apex NS drop 數、其他類型 drop 數，讓 operator 可以一眼看到「這個 zone 預期外掉了多少」。當 zone 只掉了 SOA 和 apex NS（兩種 RFC 1035 必有/必丟的記錄）時不印 summary — 那是零訊號事件，prune 乾淨後整個 ns2 應該完全安靜（ns2 規模實測：49,574 個 backup zone 預期都產生 0 條 summary）。
3. 既有 zone-parser spec Requirement 條文「records of other types in a backup-override zone SHALL be discarded with a warning」改成「discarded with a warning, **except SOA and apex NS, which SHALL be discarded silently at debug level (expected per backup-zone validity contract)**」加 INFO 摘要的條文。

## Non-Goals

- 不改 `filterBackupRecords` 的 drop / retain 決策（哪些 RR 被 drop、哪些被保留，行為完全不動）。
- 不改 alias rewrite 結果（answer 內容不變）。
- 不改 prune-backup CLI 行為（不影響 prune 規則或 dry-run / apply 流程）。
- 不引入動態 log level config（不加 flag、不讀環境變數；本 change 是 hardcode 規則）。
- 不改 zone-parser 對 root zone 的處理路徑。

## Alternatives Considered

- **完全移除 SOA/apex-NS 的 log（連 DEBUG 都不印）**：被 reject — 開 DEBUG 偵錯時希望仍可看到逐筆紀錄，方便對照「為什麼某個 owner 的 NS 沒被服務」。
- **改用 sampled WARN（每 N 條印 1 條）**：被 reject — 增加實作複雜度、訊號仍是 0、不解決根本問題。
- **保持 WARN，文件叫 operator 自行 grep 過濾**：被 reject — 37K 條已經淹沒 journal，跨啟動觀察反而被卡 disk（先前 ns2 上 /var/log 因 syslog 累積到 48GB 一度爆滿）。

## Impact

- Affected specs:
  - Modified: `openspec/specs/zone-parser/spec.md`（"Backup-Override Zone Classification" 那條 Requirement 加 SOA / apex NS 的 logging 例外條款，加一條 INFO 摘要 Requirement）
- Affected code:
  - Modified:
    - `internal/zone/classify.go` — `filterBackupRecords` 改寫 logging 分支：SOA / apex NS → DEBUG，其他類型 → WARN（不變）；累積 per-zone 計數，在每個 backup zone 處理結束處 emit 一條 INFO 摘要。
    - `internal/zone/classify_test.go` — 新增 / 調整測試覆蓋三組情境：SOA drop 走 DEBUG、apex NS drop 走 DEBUG、A/CNAME drop 仍走 WARN；驗證 INFO 摘要欄位包含 zone origin、soa_dropped、apex_ns_dropped、other_dropped 計數。
  - New: (none)
  - Removed: (none)
