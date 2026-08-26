# Environment Variables

ShadowDNS can expand process environment variables in string values in `shadowdns.yaml`. This lets one configuration template describe multiple deployments while keeping selected runtime values outside the file.

Use environment expansion for deployment-specific values such as listen addresses and for secrets only when the destination field is not normally logged. The primary secret use case is `ephemeral_api.token`.

!!! warning
    Do not inject secrets into fields whose values may appear in normal operational logs, validation messages, metrics, or generated output. Environment expansion prevents values from being disclosed by configuration-load errors, but it does not provide global redaction after a successful load. For secrets, use only fields such as `ephemeral_api.token` that are not normally logged.

---

## Process environment

Set variables in the environment of the ShadowDNS process, then reference them from value-side YAML strings:

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

`${NAME}` requires a non-empty value. If the variable is unset or empty, configuration loading fails. `${NAME:-default}` uses the literal default when the variable is unset or empty. Expansion happens once from left to right: text introduced by an environment value or default is not expanded again.

Use `$$` when a string must contain a literal dollar sign before braced text. For example, `$${API_TOKEN}` loads as the literal `${API_TOKEN}`. See [`shadowdns.yaml`](../configuration/shadowdns-yaml.md) for the complete grammar, validation behavior, and diagnostics contract.

!!! note
    Expansion reads the environment of the ShadowDNS process, not the environment of the shell that edits the configuration. When ShadowDNS runs under systemd, a container runtime, or Kubernetes, define variables in that service or workload specification.

---

## Kubernetes: Secret to Pod environment to ConfigMap YAML

Keep non-secret configuration structure in a ConfigMap and the bearer token in a Secret. The Pod injects the Secret as an environment variable; the mounted `shadowdns.yaml` references that variable.

### 1. Store the token in a Secret

`stringData` is convenient for an example because Kubernetes encodes it when creating the Secret. The resulting Secret is not encrypted merely because it is base64-encoded; enable encryption at rest and restrict RBAC access in real clusters.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: shadowdns-runtime
  namespace: dns-example
stringData:
  ephemeral-api-token: "synthetic-secret"
```

### 2. Store the configuration template in a ConfigMap

Only `ephemeral_api.token` comes from the Secret. The remaining values are ordinary, non-secret configuration.

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

### 3. Inject the Secret and mount the ConfigMap

The container receives `API_TOKEN` through `secretKeyRef`, while the ConfigMap is mounted as the unified configuration file.

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

At process startup, Kubernetes constructs the container environment from the Secret. ShadowDNS then reads `/etc/shadowdns/shadowdns.yaml` and resolves `${API_TOKEN}` from that process environment. If the Secret key is missing or resolves to an empty value, the required expression makes startup fail closed.

---

## Updating a Kubernetes Secret

Updating a Secret does **not** change the environment of an already-running container. A SIGHUP asks ShadowDNS to reload its configuration, but the process still sees the old `API_TOKEN`; SIGHUP alone therefore does not refresh an env-backed Secret.

After changing the Secret, restart the Pods so Kubernetes creates new process environments:

```bash
kubectl -n dns-example rollout restart deployment/shadowdns
kubectl -n dns-example rollout status deployment/shadowdns
```

A ConfigMap mounted as a volume may be updated independently, subject to Kubernetes projection timing. However, any configuration value sourced through Pod environment variables still requires a Pod restart. Plan token rotation as a rollout, not as a SIGHUP-only reload.

---

## Security checklist

- Put secrets only in fields that are not normally logged, such as `ephemeral_api.token`.
- Keep the Secret separate from the ConfigMap; do not copy token values into `shadowdns.yaml`.
- Restrict Kubernetes RBAC access to Secrets and enable Secret encryption at rest.
- Use `${NAME}` for required secrets so missing or empty values fail closed.
- Restart Pods after updating env-backed Secrets; do not rely on SIGHUP to refresh the process environment.
- Use synthetic values and RFC-reserved domains and addresses in shared examples and diagnostics.
