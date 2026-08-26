# 環境變數

ShadowDNS 可在 `shadowdns.yaml` 的字串值中展開 process environment variable。如此便能以同一份設定範本描述多個部署環境，同時把選定的 runtime 值留在檔案之外。

環境變數展開適合用於 listen address 等依部署而異的值；若用於機密，目標欄位必須是正常情況下不會被記錄的欄位。主要的 Secret 使用情境是 `ephemeral_api.token`。

!!! warning
    不要把機密注入可能出現在一般維運日誌、驗證訊息、metrics 或產生輸出中的欄位。環境變數展開會避免 configuration-load error 洩漏值，但成功載入後不提供全域遮罩。機密只能用於 `ephemeral_api.token` 等正常情況下不會記錄的欄位。

---

## Process environment

先在 ShadowDNS process environment 中設定變數，再從 value-side YAML 字串引用：

```bash
export API_HOST='192.0.2.10'
export API_PORT='8053'
export API_ALLOW='192.0.2.0/24'
export API_TOKEN='synthetic-secret'

go run ./cmd/shadowdns \
  --config ./shadowdns.yaml \
  --dry-run
```

```yaml
# shadowdns.yaml
ephemeral_api:
  listen: "${API_HOST}:${API_PORT}"
  allow:
    - "${API_ALLOW}"
  token: "${API_TOKEN}"

aliases:
  example.com:
    members:
      - "${BACKUP_DOMAIN:-backup.example.net}"
```

`${NAME}` 要求變數具有非空值；變數未設定或為空時，設定載入會失敗。`${NAME:-default}` 則在變數未設定或為空時使用 literal default。展開只會由左至右執行一次：environment value 或 default 所帶入的文字不會再次展開。

字串必須在 braced text 前保留 literal dollar sign 時，請使用 `$$`。例如，`$${API_TOKEN}` 載入後是 literal `${API_TOKEN}`。完整 grammar、validation behavior 與 diagnostics contract 請見 [`shadowdns.yaml`](../configuration/shadowdns-yaml.md)。

!!! note
    展開讀取的是 ShadowDNS process environment，而不是編輯設定檔之 shell 的 environment。ShadowDNS 在 systemd、container runtime 或 Kubernetes 下執行時，請在該 service 或 workload specification 中定義變數。

---

## Kubernetes：Secret → Pod environment → ConfigMap YAML

把非機密的設定結構放在 ConfigMap，bearer token 則放在 Secret。Pod 將 Secret 注入 environment variable，而掛載的 `shadowdns.yaml` 再引用該變數。

### 1. 將 token 存入 Secret

範例使用 `stringData`，因為 Kubernetes 會在建立 Secret 時進行編碼。產生的 Secret 並不會只因 base64 編碼而成為加密資料；實際 cluster 應啟用 encryption at rest 並限制 RBAC 存取。

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: shadowdns-runtime
  namespace: dns-example
stringData:
  ephemeral-api-token: "synthetic-secret"
```

### 2. 將設定範本存入 ConfigMap

只有 `ephemeral_api.token` 來自 Secret，其餘皆為一般、非機密設定。

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: shadowdns-config
  namespace: dns-example
data:
  shadowdns.yaml: |
    aliases:
      example.com:
        members:
          - backup.example.net
    ephemeral_api:
      listen: "192.0.2.10:8053"
      allow:
        - "192.0.2.0/24"
      token: "${API_TOKEN}"
```

### 3. 注入 Secret 並掛載 ConfigMap

Container 透過 `secretKeyRef` 取得 `API_TOKEN`，ConfigMap 則掛載成 unified configuration file。

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shadowdns
  namespace: dns-example
spec:
  replicas: 1
  selector:
    matchLabels:
      app: shadowdns
  template:
    metadata:
      labels:
        app: shadowdns
    spec:
      containers:
        - name: shadowdns
          image: registry.example.org/shadowdns:0.1.0
          args:
            - "--config"
            - "/etc/shadowdns/shadowdns.yaml"
          env:
            - name: API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: shadowdns-runtime
                  key: ephemeral-api-token
          volumeMounts:
            - name: config
              mountPath: /etc/shadowdns
              readOnly: true
      volumes:
        - name: config
          configMap:
            name: shadowdns-config
```

Process 啟動時，Kubernetes 會依 Secret 建立 container environment。ShadowDNS 接著讀取 `/etc/shadowdns/shadowdns.yaml`，並從該 process environment 解析 `${API_TOKEN}`。若 Secret key 不存在或解析成空值，required expression 會使啟動 fail closed。

---

## 更新 Kubernetes Secret

更新 Secret **不會**改變已在執行之 container 的 environment。SIGHUP 會要求 ShadowDNS reload 設定，但 process 仍只看得到舊的 `API_TOKEN`；因此，單獨發送 SIGHUP 不會刷新 env-backed Secret。

變更 Secret 後，請重啟 Pod，讓 Kubernetes 建立新的 process environment：

```bash
kubectl -n dns-example rollout restart deployment/shadowdns
kubectl -n dns-example rollout status deployment/shadowdns
```

以 volume 掛載的 ConfigMap 可在 Kubernetes projection timing 的限制下獨立更新；然而，任何透過 Pod environment variable 提供的設定值仍需重啟 Pod。Token rotation 應規劃成 rollout，而不是只執行 SIGHUP reload。

---

## 安全檢查清單

- 機密只放在 `ephemeral_api.token` 等正常情況下不會記錄的欄位。
- Secret 與 ConfigMap 分開管理；不要把 token value 複製進 `shadowdns.yaml`。
- 限制 Kubernetes Secret 的 RBAC 存取，並啟用 Secret encryption at rest。
- Required secret 使用 `${NAME}`，使遺漏或空值能 fail closed。
- 更新 env-backed Secret 後重啟 Pod；不要依賴 SIGHUP 刷新 process environment。
- 共享範例與 diagnostics 只使用 synthetic value、RFC 保留 domain 與 address。
