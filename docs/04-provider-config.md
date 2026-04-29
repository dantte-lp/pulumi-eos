# Provider configuration

## Schema (preview · finalised in S3)

```yaml
config:
  eos:url:           # eAPI base URL or gNMI host:port (string)
  eos:username:      # optional (string)
  eos:password:      # optional (Secret string)
  eos:tlsCaCert:     # PEM (Secret string) or path
  eos:tlsClientCert: # PEM (Secret string) or path · for mTLS
  eos:tlsClientKey:  # PEM (Secret string) or path · for mTLS
  eos:insecure:      # bool · disables TLS verification (dev only)
  eos:transport:     # enum: eapi | gnmi · default: eapi
  eos:requestTimeout:  # duration · default: 30s
  eos:sessionPrefix:   # string · default: pulumi-

  cvp:url:           # CVP / CVaaS base URL (string)
  cvp:token:         # service-account bearer token (Secret string)
  cvp:caCert:        # PEM (Secret string) or path

  retry:maxAttempts: # int · default: 5
  retry:baseDelay:   # duration · default: 1s
  retry:maxDelay:    # duration · default: 30s
```

## Environment variables

| Variable | Maps to |
|---|---|
| `EOS_URL` | `eos:url` |
| `EOS_USERNAME` | `eos:username` |
| `EOS_PASSWORD` | `eos:password` |
| `EOS_TLS_CA_CERT` | `eos:tlsCaCert` |
| `EOS_TLS_CLIENT_CERT` | `eos:tlsClientCert` |
| `EOS_TLS_CLIENT_KEY` | `eos:tlsClientKey` |
| `EOS_INSECURE` | `eos:insecure` |
| `EOS_TRANSPORT` | `eos:transport` |
| `CVP_URL` | `cvp:url` |
| `CVP_TOKEN` | `cvp:token` |
| `CVP_CA_CERT` | `cvp:caCert` |

## Secrets

| Field | Type | Storage |
|---|---|---|
| `eos:password` | Secret string | Pulumi state encryption. |
| `eos:tlsClientKey` | Secret string | Pulumi state encryption. |
| `cvp:token` | Secret string | Pulumi state encryption. |
