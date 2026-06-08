# 效能基準數據

## 1.3 — `internal/cookie` BenchmarkGenerate

執行指令：`go test -bench=Generate -benchmem ./internal/cookie`

測試機器：Apple M4（darwin/arm64）

```
BenchmarkGenerate-10    105062000    11.69 ns/op    0 B/op    0 allocs/op
```

單次 server cookie 產生（SipHash-2-4 + 24-byte 組裝）約 11.7 ns、零堆積配置，對 hot path 的影響可忽略。

## 4.1 — `internal/server` ServeDNS 三路徑微基準

執行指令：`go test -bench=ServeDNS -benchmem -count=3 ./internal/server`

測試機器：Apple M4（darwin/arm64）

### 實作前（baseline，handler.go 未修改）

```
BenchmarkServeDNS_NoEDNS-10            	 2345587	       490.4 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_NoEDNS-10            	 2542074	       470.8 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_NoEDNS-10            	 2373390	       490.7 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2457814	       488.5 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2452996	       483.8 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2516652	       475.1 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 2523525	       475.7 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 2533552	       475.3 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 2527617	       474.4 ns/op	     936 B/op	      12 allocs/op
```

實作前三路徑相同（cookie/OPT 尚未處理）：約 470–490 ns/op、936 B/op、12 allocs/op。

### 實作後（OPT echo + cookie 完成，含 request-OPT 重用優化）

```
BenchmarkServeDNS_NoEDNS-10            	 2454817	       488.2 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_NoEDNS-10            	 2472012	       486.0 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_NoEDNS-10            	 2464676	       486.6 ns/op	     936 B/op	      12 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2187784	       549.4 ns/op	     984 B/op	      13 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2209023	       544.1 ns/op	     984 B/op	      13 allocs/op
BenchmarkServeDNS_EDNSNoCookie-10      	 2193126	       547.1 ns/op	     984 B/op	      13 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 1538025	       778.3 ns/op	    1328 B/op	      22 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 1535733	       781.0 ns/op	    1328 B/op	      22 allocs/op
BenchmarkServeDNS_EDNSWithCookie-10    	 1528400	       782.2 ns/op	    1328 B/op	      22 allocs/op
```

（cookie 路徑數據為 simplify 後複測：hex 先驗長度再僅解碼前 8 bytes，省一次 heap 配置，794→780 ns、23→22 allocs。）

### 前後對照

| 路徑 | 實作前 | 實作後 | 差異 |
|---|---|---|---|
| 無 EDNS | ~484 ns / 936 B / 12 allocs | ~487 ns / 936 B / 12 allocs | 噪音內，零退化 |
| 有 EDNS 無 cookie | ~482 ns / 936 B / 12 allocs | ~547 ns / 984 B / 13 allocs | +65 ns / +48 B / +1 alloc |
| 有 cookie | ~475 ns（當時等同 EDNS 路徑） | ~780 ns / 1328 B / 22 allocs | 新功能路徑 |

EDNS 路徑的 +65 ns 是 OPT echo（修復 RFC 6891 合規缺口）的必要新工作：回應多打包一個 OPT RR（11 bytes wire）＋ Extra slice 配置。已做優化：attachOPT 重用 request 的 OPT record，省去 per-response OPT 配置（562→547 ns、14→13 allocs）。cookie 路徑的額外 ~247 ns 為 hex decode/encode、SipHash-2-4 與 COOKIE option 組裝。

### 本機 loopback A/B 壓測（系統層預檢，非正式驗收）

dnspyre `-c 4 -d 5s -t A --edns0=1232`，對 smoke fixture（`www.example.com`），各 3 輪：

| 情境 | QPS（3 輪） | 平均 | p99 |
|---|---|---|---|
| 實作前（EDNS 無 cookie） | 97,049 / 98,808 / 104,659 | ~100,172 | 106.5–122.9 µs |
| 實作後（EDNS 無 cookie） | 103,012 / 104,182 / 103,771 | ~103,655 | 98.3–106.5 µs |
| 實作後（帶 COOKIE，`--ednsopt=10:...`） | 101,115 / 102,583 / 101,524 | ~101,740 | 102.4–106.5 µs |

run-to-run 噪音約 ±4%，三種情境統計上不可區分——系統層 QPS 無可測退化。handler 的 +65 ns 占每查詢總成本（~10 µs，syscall 主導）約 0.7%。`dig +cookie` 回報 `(good)`，確認 RFC 9018 server cookie 格式正確。正式驗收以 4.3/4.4 的跨網路壓測為準。
