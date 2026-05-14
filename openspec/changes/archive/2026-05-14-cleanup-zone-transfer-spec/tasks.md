## 1. 修復主 spec 結構

- [x] 1.1 刪除 [openspec/specs/zone-transfer/spec.md](openspec/specs/zone-transfer/spec.md) 從第二份重複區塊開頭（line 697 起、目前以 `### Requirement: Serve AXFR over TCP for loaded zones` 第二次出現為起點）到 EOF 的所有內容，使主 spec 只剩第一份 Requirements（保留每段 `<!-- @trace -->` audit blocks）
- [x] 1.2 在同一檔的第一份 `### Requirement: Send NOTIFY on zone content change`（line 284 附近）原地替換 Requirement 本文與全部 scenarios，使其與本 change spec delta 的 MODIFIED 區塊內容**逐字一致**——同時涵蓋 glue resolution 與 notify-toggle 兩組行為的 12 個 scenarios。**不要動該 Requirement 後方原有的 3 個 `<!-- @trace -->` 區塊**（shadowdns-foundation / notify-toggle / notify-glue-resolution），保留它們做為歷史 audit 之用
- [x] 1.3 確認改完後 [openspec/specs/zone-transfer/spec.md](openspec/specs/zone-transfer/spec.md) 只剩下單一份 6 個 Requirements（AXFR、alias AXFR、allow-transfer、Send NOTIFY、IXFR fallback、Refuse unknown）；以 `grep -nE "^### Requirement" openspec/specs/zone-transfer/spec.md` 驗證輸出恰有 6 行
- [x] 1.4 跑 `spectra validate cleanup-zone-transfer-spec` 確認 change 仍 valid，並跑 `spectra ask "list scenarios under Send NOTIFY on zone content change"` 或直接讀檔確認新 NOTIFY Requirement 收齊 12 個 scenarios（6 glue + 6 toggle）
