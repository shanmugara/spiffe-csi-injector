# spiffe-csi-injector-chart

Deploys the SPIFFE CSI mutating admission webhook: `Deployment`, `Service`,
`ServiceAccount` + namespaced `Role`/`RoleBinding`, a cert-manager
`Certificate` for its serving cert, and the cluster-scoped
`MutatingWebhookConfiguration` (`csi-injector-webhook.omegahome.net`).

## Install

```sh
helm install csi-injector ./spiffe-csi-injector-chart \
  --namespace spire --create-namespace
```

Only namespaces labeled `csi-webhook: enabled` are affected by this webhook
(`webhook.namespaceSelector`) - label a namespace to opt it in:

```sh
kubectl label namespace <ns> csi-webhook=enabled
```

## Values

| Key | Default | Description |
|---|---|---|
| `image.repository` / `.tag` | `docker.io/shanmugara/spiffe-csi-injector` / *(Chart appVersion)* | Webhook image |
| `serviceAccount.name` | `spiffe-csi-injector` | Fixed - matches the RBAC binding this chart also creates |
| `webhook.failurePolicy` | `Fail` | Safe here because the webhook only affects opted-in (labeled) namespaces, unlike a cluster-wide opt-out webhook |
| `webhook.namespaceSelector` | `matchLabels: {csi-webhook: "enabled"}` | Which namespaces this webhook applies to |
| `certificate.issuerRef.name` / `.kind` | `cluster-ca-issuer` / `ClusterIssuer` | cert-manager issuer for the webhook's serving cert |
| `webhookWait.enabled` | `true` | Gates the `MutatingWebhookConfiguration` behind a post-install/post-upgrade Job that waits for a `csi-injector` pod to be Ready first - see below |
| `extraEnvConfigMapName` | `spiffe-csi-injector-extra-env` | Name of an optional ConfigMap (in this chart's own namespace) supplying extra env vars injected into every mutated container. Not created by this chart. |

## RBAC scope

The webhook only ever looks up its extra-env ConfigMap in **its own
namespace** (via the `POD_NAMESPACE` downward-API env var) - never in the
namespace of the pod it's mutating. So the `Role`/`RoleBinding` this chart
creates are namespaced, not cluster-wide; no `ClusterRole` is needed.

## Why `webhookWait`

Helm applies resources in a fixed kind order but doesn't wait for pod
readiness in between by default - the `MutatingWebhookConfiguration` could
otherwise exist and start intercepting admission requests before the
`Deployment`'s pod has even pulled its image. `webhookWait` defers the
webhook config to a `post-install,post-upgrade` hook (weight `"0"`) that
only runs after a lower-weighted (`"-5"`) Job confirms a `csi-injector` pod
is actually `Ready`. Same pattern as `ca-bundle-injector`'s chart.
