# Reloading ShadowDNS

ShadowDNS supports transactional configuration reloads with SIGHUP. A successful
reload replaces reloadable server state without stopping DNS service; a failed
reload leaves the active state unchanged. Some settings create process-lifetime
listeners and therefore still require a restart.

## Trigger a reload

With the packaged systemd unit, use:

```bash
sudo systemctl reload shadowdns
```

For a container, send SIGHUP to the ShadowDNS process. For example:

```bash
docker kill --signal HUP shadowdns
```

Before reloading, confirm that no DNS-01 challenge is in progress. A successful
reload clears all records held by the in-memory Ephemeral TXT store. A failed
reload keeps those records.

## What SIGHUP updates

Every SIGHUP invokes the unified configuration loader again. ShadowDNS reads the
configuration files, expands environment expressions in `shadowdns.yaml`, and
validates the complete candidate configuration before swapping the active
reloadable state.

Reloadable state includes:

- zone data and alias mappings;
- GeoIP databases;
- rate-limit settings;
- query-log settings; and
- other server state derived by the existing reload path.

All fallible loading and validation work finishes before the state swap. If an
environment expression cannot be resolved, expanded data is invalid, or another
reload step fails, ShadowDNS continues serving with the previous state and
increments the reload failure metric. Diagnostics identify safe environment
variable names and YAML paths without printing environment-derived values.

## What SIGHUP does not update

The Ephemeral API and DNS-over-HTTPS (DoH) servers are **startup-scoped**. Their
listeners and configuration objects are created when the ShadowDNS process
starts. Although SIGHUP reloads and validates their YAML sections through the
unified loader, it does not replace the already-running API or DoH server.

Use a full process restart to apply changes such as:

- enabling or disabling the Ephemeral API or DoH;
- changing either service's listen address;
- changing Ephemeral API access rules or token; and
- changing DoH or ACME settings.

A successful SIGHUP can still change zone origins consulted by the running
Ephemeral API, because those checks use reloadable zone state. It also clears the
Ephemeral TXT store. Neither behavior means that the API listener or its
startup-scoped configuration was replaced.

CLI flags and DNS listen-address changes are also process-scoped. Restart
ShadowDNS after changing them.

## Process environment on reload

Environment expansion reads the environment visible to the **existing
ShadowDNS process at the moment the loader runs**. Consequently, SIGHUP can
observe a variable changed through a process supervisor or another mechanism
only if that mechanism actually changes the running process's environment.
Changing an interactive shell, an environment file on disk, or a deployment
manifest does not by itself alter an already-running process.

For example, if an alias target uses `${ALIAS_TARGET}`, a reload applies the
current process-visible value to the reloadable alias state:

```yaml
aliases:
  example.com:
    members:
      - "${ALIAS_TARGET}"
```

If `ALIAS_TARGET` is missing or empty, the reload fails closed and the previous
alias state remains active.

## Kubernetes Secrets used as environment variables

Kubernetes injects a Secret-backed environment variable when it starts a
container. Updating the Secret object does **not** update the environment of an
existing Pod process. Sending only SIGHUP after such an update therefore reloads
the old process-visible value, not the new Secret value.

After updating a Secret consumed through `env` or `envFrom`, restart the Pods:

```bash
kubectl apply -f shadowdns-secret.yaml
kubectl rollout restart deployment/shadowdns
kubectl rollout status deployment/shadowdns
```

The replacement Pods receive the updated environment at startup and create new
startup-scoped listeners from the same configuration. Use a rolling rollout for
multi-replica deployments so healthy replicas continue serving traffic.

!!! warning "SIGHUP is not a Secret refresh mechanism"
    A Secret update followed by `systemctl reload`, `docker kill --signal HUP`,
    or a direct SIGHUP is insufficient for an existing Kubernetes Pod when the
    Secret is exposed as environment variables. Perform a Pod rollout restart.

A Secret mounted as a volume has different Kubernetes file-update behavior, but
ShadowDNS still reads only the sources referenced by its configuration loader.
Do not infer environment refresh from a projected file changing: if the YAML
uses `${NAME}`, the value comes from the process environment and requires a new
Pod process to receive an updated Secret-backed value.

## Operational decision table

| Change | SIGHUP | Restart / rollout |
|--------|---------|-------------------|
| Zone or alias state | Applies after a successful reload | Not normally required |
| GeoIP, rate-limit, or query-log state | Applies after a successful reload | Not normally required |
| Environment value already changed in the running process | Re-read by the unified loader | Depends on how the environment was changed |
| Kubernetes Secret exposed through `env` / `envFrom` | Does not obtain the updated Secret value | **Pod rollout required** |
| Ephemeral API configuration | Validated, but running server is unchanged | **Required to apply** |
| DoH / ACME configuration | Validated, but running server is unchanged | **Required to apply** |
| DNS or HTTP listen addresses, or CLI flags | Running listeners / flags are unchanged | **Required to apply** |

After either a reload or rollout, verify DNS answers and monitor
`shadowdns_reload_total{result="failure"}` together with
`shadowdns_config_last_reload_success_timestamp_seconds`. See
[Monitoring with Prometheus and Grafana](monitoring.md) for the complete metric
reference.
